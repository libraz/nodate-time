package eventexpand

import (
	"context"
	"database/sql"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/recurrence"
)

type ExceptionLoader interface {
	ListRecurrenceExceptionsByParent(ctx context.Context, recurrenceParentID sql.NullInt32) ([]generated.Event, error)
}

type Instance struct {
	Event         generated.Event
	Occurrence    recurrence.Occurrence
	OriginalStart time.Time
	IsException   bool
}

type exceptionInfo struct {
	child         generated.Event
	cancelled     bool
	originalStart time.Time
}

type exceptionSet struct {
	byInstant map[int64]exceptionInfo
	byDate    map[string]exceptionInfo
}

func loadExceptions(ctx context.Context, loader ExceptionLoader, parentID uint32) exceptionSet {
	rows, err := loader.ListRecurrenceExceptionsByParent(ctx, sql.NullInt32{Int32: int32(parentID), Valid: true})
	if err != nil || len(rows) == 0 {
		return exceptionSet{}
	}
	exceptions := exceptionSet{
		byInstant: make(map[int64]exceptionInfo, len(rows)),
		byDate:    make(map[string]exceptionInfo, len(rows)),
	}
	for _, r := range rows {
		if !r.RecurrenceOriginalStart.Valid {
			continue
		}
		originalStart := r.RecurrenceOriginalStart.Time.UTC()
		ex := exceptionInfo{child: r, cancelled: r.RecurrenceCancelled, originalStart: originalStart}
		exceptions.byInstant[originalStart.UnixMilli()] = ex
		exceptions.byDate[originalStart.Format("20060102")] = ex
	}
	return exceptions
}

func (s exceptionSet) find(occ recurrence.Occurrence) (exceptionInfo, bool) {
	key := occ.StartAt.UTC().UnixMilli()
	if ex, ok := s.byInstant[key]; ok {
		return ex, true
	}
	ex, ok := s.byDate[occ.StartAt.UTC().Format("20060102")]
	return ex, ok
}

func ExpandRecurringEvent(
	ctx context.Context,
	loader ExceptionLoader,
	event generated.Event,
	windowStart time.Time,
	windowEnd time.Time,
) []Instance {
	if event.RecurrenceRule == nil {
		return nil
	}
	rule := recurrence.ParseRule(*event.RecurrenceRule)
	if rule == nil {
		return nil
	}

	exceptions := loadExceptions(ctx, loader, event.ID)
	consumed := make(map[int64]bool, len(exceptions.byInstant))
	occurrences := recurrence.ExpandInZone(rule, event.StartAt, event.EndAt, windowStart, windowEnd, event.Timezone)
	instances := make([]Instance, 0, len(occurrences))

	for _, occ := range occurrences {
		if ex, ok := exceptions.find(occ); ok {
			consumed[ex.originalStart.UnixMilli()] = true
			if ex.cancelled {
				continue
			}
			instances = append(instances, Instance{
				Event:         ex.child,
				Occurrence:    occ,
				OriginalStart: ex.originalStart,
				IsException:   true,
			})
			continue
		}
		instances = append(instances, Instance{
			Event:         event,
			Occurrence:    occ,
			OriginalStart: occ.StartAt.UTC(),
		})
	}

	for key, ex := range exceptions.byInstant {
		if consumed[key] || ex.cancelled {
			continue
		}
		if ex.child.StartAt.Before(windowStart) || !ex.child.StartAt.Before(windowEnd) {
			continue
		}
		instances = append(instances, Instance{
			Event:         ex.child,
			OriginalStart: ex.originalStart,
			IsException:   true,
		})
	}
	return instances
}
