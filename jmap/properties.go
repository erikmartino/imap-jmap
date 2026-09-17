package jmap

import (
	"imap-jmap/jmap/jmaphandler"
)

var (
	parseProperties  = jmaphandler.ParseProperties
	ParseProperties  = jmaphandler.ParseProperties
	filterProperties = jmaphandler.FilterProperties
	FilterProperties = jmaphandler.FilterProperties
)

// FilterList applies FilterProperties to every element of a typed list.
func FilterList[T any](list []*T, properties []string) []any {
	return jmaphandler.FilterList(list, properties)
}

func filterList[T any](list []*T, properties []string) []any {
	return jmaphandler.FilterList(list, properties)
}
