package dockertest

var ElasticSearch2 = Specification{
	Image:  "elasticsearch:2",
	Waiter: RegexWaiter("\\[node *\\].* started"),
	Services: SimpleServiceMap{
		"main": SimpleService(9200, "{{.}}"),
	},
}
