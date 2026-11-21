package router

var searchConventions = []convention{
	read("search-events", "/search", listWindow),
}

func init() { operationConventions = append(operationConventions, searchConventions...) }
