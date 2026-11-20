// Package timetree migrates shared TimeTree calendars into this deployment.
//
// Run logs in to TimeTree, fetches every active calendar with its labels and
// events, checks the operator's mapping file against what it fetched and what
// the database holds, and only then writes: accounts, calendars, members and
// events, with each event filed under the owner its label maps to. Any
// problem found before the first write ends the run with exit code 2 and
// leaves the database as it was.
//
// Exit codes: 0 success, 1 at least one write failed, 2 nothing was written.
// A dry run performs every step up to the first write and prints a report.
package timetree

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"

	"github.com/libraz/nodate-time/apps/api/internal/accounts"
	"github.com/libraz/nodate-time/apps/api/internal/calimport"
	"github.com/libraz/nodate-time/apps/api/internal/config"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/dbtx"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/calresolve"
	"github.com/libraz/nodate-time/apps/api/internal/workspace"
)

// Exit codes returned by Run.
const (
	exitOK       = 0
	exitFailed   = 1
	exitNoWrites = 2
)

const allowlistNote = "created by importtimetree"

// Options configures one run.
type Options struct {
	// BaseURL is the TimeTree origin; empty means https://timetreeapp.com.
	BaseURL string
	// Email and Password are the TimeTree login; both are required.
	Email, Password string
	// Mapping is the YAML mapping file.
	Mapping io.Reader
	DryRun  bool
	DB      *sql.DB
	Config  *config.Config
	// Out receives the report.
	Out io.Writer
}

// Run executes the import. The error explains a non-zero exit code.
func Run(ctx context.Context, o Options) (int, error) {
	out := o.Out
	if out == nil {
		out = io.Discard
	}
	if o.Email == "" || o.Password == "" {
		return exitNoWrites, errors.New("TIMETREE_EMAIL and TIMETREE_PASSWORD must be set")
	}
	m, err := parseMapping(o.Mapping)
	if err != nil {
		return exitNoWrites, err
	}
	if errs := m.validate(o.DryRun); len(errs) > 0 {
		writeErrors(out, errs)
		return exitNoWrites, fmt.Errorf("mapping has %d error(s)", len(errs))
	}

	if o.DB == nil || o.Config == nil {
		return exitNoWrites, errors.New("database and configuration are required")
	}

	q := generated.New(o.DB)
	ws, found, err := resolveWorkspace(ctx, q, o.Config, o.DryRun)
	if err != nil {
		return exitNoWrites, err
	}
	if _, ok := readZone(ws.Timezone); !ok {
		return exitNoWrites, fmt.Errorf("workspace timezone %q is not an IANA zone name", ws.Timezone)
	}

	data, err := fetch(ctx, o)
	if err != nil {
		return exitNoWrites, err
	}

	// Conversion needs the workspace zone; resolution needs the database.
	lk := dbLookup{q: q, workspaceID: ws.ID, found: found}
	labels := map[flexID][]ttLabel{}
	cals := make([]ttCalendar, len(data))
	for i, d := range data {
		labels[d.cal.ID] = d.labels
		cals[i] = d.cal
	}
	res, err := resolve(ctx, m, cals, labels, lk)
	if err != nil {
		return exitNoWrites, err
	}

	plans := map[flexID]*calendarPlan{}
	for i := range res.Plans {
		plans[res.Plans[i].TT.ID] = &res.Plans[i]
	}
	analyses := make([]*analysis, len(data))
	for i, d := range data {
		var mc *MappingCalendar
		if p := plans[d.cal.ID]; p != nil {
			mc = &p.Mapping
		}
		cv := newConverter(ws.Timezone, d.labels, mc, m.Importer)
		analyses[i] = analyze(d, cv)
	}

	if o.DryRun {
		writeReport(out, analyses, res)
		if len(res.Errors) > 0 {
			return exitNoWrites, fmt.Errorf("mapping has %d error(s) against TimeTree and the database", len(res.Errors))
		}
		return exitOK, nil
	}
	if len(res.Errors) > 0 {
		writeErrors(out, res.Errors)
		return exitNoWrites, fmt.Errorf("mapping has %d error(s) against TimeTree and the database", len(res.Errors))
	}
	return write(ctx, o, ws, m, res, analyses)
}

// resolveWorkspace finds the workspace. A real run creates it when missing;
// a dry run must not, and reports found=false instead.
func resolveWorkspace(ctx context.Context, q *generated.Queries, cfg *config.Config, dryRun bool) (workspace.Scope, bool, error) {
	if !dryRun {
		ws, err := workspace.Ensure(ctx, q, cfg.WorkspaceSlug, cfg.WorkspaceName, cfg.WorkspaceTimezone, cfg.WorkspaceCountry)
		if err != nil {
			return workspace.Scope{}, false, fmt.Errorf("resolve workspace: %w", err)
		}
		return ws, true, nil
	}
	row, err := q.GetWorkspaceBySlug(ctx, cfg.WorkspaceSlug)
	switch {
	case err == nil:
		return workspace.Scope{ID: row.ID, PublicID: row.PublicID, Slug: row.Slug, Timezone: row.Timezone}, true, nil
	case errors.Is(err, sql.ErrNoRows):
		return workspace.Scope{Slug: cfg.WorkspaceSlug, Timezone: cfg.WorkspaceTimezone}, false, nil
	default:
		return workspace.Scope{}, false, fmt.Errorf("resolve workspace: %w", err)
	}
}

// fetch logs in and reads every active calendar completely.
func fetch(ctx context.Context, o Options) ([]calData, error) {
	c, err := newClient(o.BaseURL)
	if err != nil {
		return nil, err
	}
	if err := c.login(ctx, o.Email, o.Password); err != nil {
		return nil, err
	}
	cals, err := c.calendars(ctx)
	if err != nil {
		return nil, err
	}
	data := make([]calData, 0, len(cals))
	for _, cal := range cals {
		labels, err := c.labels(ctx, cal.ID)
		if err != nil {
			return nil, err
		}
		events, err := c.events(ctx, cal.ID)
		if err != nil {
			return nil, err
		}
		data = append(data, calData{cal: cal, labels: labels, events: events})
	}
	return data, nil
}

// counts are the per-calendar results of a real run.
type counts struct {
	imported, duplicates, skipped, failed, rejected int
}

func (c *counts) add(o counts) {
	c.imported += o.imported
	c.duplicates += o.duplicates
	c.skipped += o.skipped
	c.failed += o.failed
	c.rejected += o.rejected
}

// write performs the mutations in dependency order.
func write(ctx context.Context, o Options, ws workspace.Scope, m *Mapping, res resolution, analyses []*analysis) (int, error) {
	q := generated.New(o.DB)
	out := o.Out
	if out == nil {
		out = io.Discard
	}

	created := 0
	for _, u := range m.Users {
		r, err := accounts.Create(ctx, o.DB, o.Config, accounts.Params{
			Email: u.Email, Name: u.Name, SkipExisting: true, AllowlistNote: allowlistNote,
		})
		if err != nil {
			return exitFailed, fmt.Errorf("create user %s: %w", u.Email, err)
		}
		if r.Created {
			created++
		}
	}

	users := map[string]generated.User{}
	for _, r := range m.refs() {
		if _, ok := users[norm(r.email)]; ok {
			continue
		}
		u, err := q.GetUserByEmail(ctx, r.email)
		if err != nil {
			return exitFailed, fmt.Errorf("look up %s: %w", r.email, err)
		}
		users[norm(r.email)] = u
	}
	importer := users[norm(m.Importer)]

	analysisOf := map[flexID]*analysis{}
	for _, a := range analyses {
		analysisOf[a.data.cal.ID] = a
	}

	var total counts
	calendarsCreated := 0
	for _, p := range res.Plans {
		var cal generated.Calendar
		var err error
		if p.ExistingID != 0 {
			cal, err = q.GetCalendarByID(ctx, p.ExistingID)
		} else {
			err = dbtx.Run(ctx, o.DB, func(tq *generated.Queries) error {
				var e error
				cal, e = calresolve.SetUp(ctx, tq, ws.ID, importer.ID, p.Name, "")
				return e
			})
			calendarsCreated++
		}
		if err != nil {
			return exitFailed, fmt.Errorf("calendar %q: %w", p.Name, err)
		}

		added, existing := 0, 0
		for _, mem := range p.Mapping.Members {
			target := users[norm(mem.Email)]
			err := dbtx.Run(ctx, o.DB, func(tq *generated.Queries) error {
				_, e := calresolve.AddMember(ctx, tq, cal, importer.ID, target, mappingRoles[mem.Role])
				return e
			})
			switch {
			case err == nil:
				added++
			case errors.Is(err, apierrors.MemberAlreadyAdded):
				existing++
			default:
				return exitFailed, fmt.Errorf("calendar %q: add member %s: %w", p.Name, mem.Email, err)
			}
		}

		a := analysisOf[p.TT.ID]
		var events []calimport.Event
		var c counts
		for _, cv := range a.conv {
			switch {
			case cv.Skip != "":
				c.skipped++
			case cv.Reject != "":
				c.failed++
				c.rejected++
			default:
				ev := cv.Event
				ev.OwnerUserID = users[cv.Owner].ID
				for _, email := range cv.Attendees {
					ev.AttendeeUserIDs = append(ev.AttendeeUserIDs, users[email].ID)
				}
				events = append(events, ev)
			}
		}
		r := calimport.Run(ctx, o.DB, ws.ID, cal, importer.ID, events, sourceTimeTree)
		c.add(counts{r.Imported, r.Duplicates, r.Skipped, r.Failed, r.Rejected})
		total.add(c)

		state := "reused"
		if p.ExistingID == 0 {
			state = "created"
		}
		fmt.Fprintf(out, "calendar %q (%s): members added=%d existing=%d; events imported=%d duplicates=%d skipped=%d failed=%d rejected=%d\n",
			p.Name, state, added, existing, c.imported, c.duplicates, c.skipped, c.failed, c.rejected)
	}
	fmt.Fprintf(out, "total: imported=%d duplicates=%d skipped=%d failed=%d rejected=%d calendars_created=%d users_created=%d\n",
		total.imported, total.duplicates, total.skipped, total.failed, total.rejected, calendarsCreated, created)

	if total.failed > 0 {
		return exitFailed, fmt.Errorf("%d event(s) failed to import", total.failed)
	}
	return exitOK, nil
}
