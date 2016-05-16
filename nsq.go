package dockertest

var NSQd = Specification{
	Image:          "nsqio/nsq",
	ImageArguments: []string{"/nsqd"},
	Waiter:         RegexWaiter("HTTP: listening on "),
	Services: SimpleServiceMap{
		"tcp":  SimpleService(4150, "{{.}}"),
		"http": SimpleService(4151, "http://{{.}}"),
	},
}

var NSQLookupd = Specification{
	Image:          "nsqio/nsq",
	ImageArguments: []string{"/nsqlookupd"},
	Waiter:         RegexWaiter("HTTP: listening on "),
	Services: SimpleServiceMap{
		"tcp":  SimpleService(4160, "{{.}}"),
		"http": SimpleService(4161, "http://{{.}}"),
	},
}
