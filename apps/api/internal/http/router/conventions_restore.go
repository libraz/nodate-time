package router

import "net/http"

var restoreConventions = []convention{
	action("restore-event", eventPath+"/restore", http.StatusOK,
		"undoes a deletion; a second restore is refused as not deleted"),
	remove("undo-event-exception", eventPath+"/exceptions/{occurrenceDate}"),
}

func init() { operationConventions = append(operationConventions, restoreConventions...) }
