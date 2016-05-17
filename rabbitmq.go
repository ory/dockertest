package dockertest

var RabbitMQ3 = Specification{
	Image:  "rabbitmq:3",
	Waiter: RegexWaiter("Server startup complete;"),
	Services: SimpleServiceMap{
		"main": SimpleService(5672, "amqp://{{.}}"),
	},
}
