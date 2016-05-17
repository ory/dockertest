package dockertest

var RethinkDB2 = Specification{
	Image:  "rethinkdb:2",
	Waiter: RegexWaiter("Server ready, "),
	Services: SimpleServiceMap{
		"main": SimpleService(28015, "{{.}}"),
	},
}
