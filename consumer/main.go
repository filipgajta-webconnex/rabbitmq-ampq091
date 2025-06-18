package main

import (
	"fmt"
	"log"
	"math/rand"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	exchangeName  = "example_exchange"
	dlxExchange   = "dlx_exchange"
	queueName     = "example_queue"
	dlqQueue      = "dlq_queue"
	routingKey    = "example_key"
	dlxRoutingKey = "dlq_key"
	queueTTL      = 6000 // ms
)

// MessageStatus for colored output
type MessageStatus struct {
	Timestamp  string
	Status     string
	Message    string
	StatusType string // "success", "fail", "dlq"
}

func printMessageStatus(status MessageStatus) {
	var colorCode string
	switch status.StatusType {
	case "success":
		colorCode = "\033[32m"
	case "fail":
		colorCode = "\033[33m"
	case "dlq":
		colorCode = "\033[31m"
	default:
		colorCode = "\033[0m"
	}
	fmt.Printf("│ \033[90m%-19s\033[0m │ %s%-14s\033[0m │ %-35s │\n",
		status.Timestamp, colorCode, status.Status, status.Message)
	fmt.Printf("├─────────────────────┼────────────────┼─────────────────────────────────────┤\n")
}

func failOnError(err error, msg string) {
	if err != nil {
		log.Fatalf("%s: %v", msg, err)
	}
}

func main() {
	conn, err := amqp.Dial("amqp://guest:guest@localhost:5672/")
	failOnError(err, "Failed to connect to RabbitMQ")
	defer conn.Close()

	ch, err := conn.Channel()
	failOnError(err, "Failed to open a channel")
	defer ch.Close()

	// Exchanges
	failOnError(ch.ExchangeDeclare(exchangeName, "topic", true, false, false, false, nil), "Declare main exchange")
	failOnError(ch.ExchangeDeclare(dlxExchange, "topic", true, false, false, false, nil), "Declare DLX exchange")

	// Main queue: TTL + dead-letter to DLX
	args := amqp.Table{
		"x-message-ttl":             int32(queueTTL),
		"x-dead-letter-exchange":    dlxExchange,
		"x-dead-letter-routing-key": dlxRoutingKey,
	}
	_, err = ch.QueueDeclare(queueName, true, false, false, false, args)
	failOnError(err, "Declare main queue")
	failOnError(ch.QueueBind(queueName, routingKey, exchangeName, false, nil), "Bind main queue")

	// DLQ: final destination for dead-letters
	_, err = ch.QueueDeclare(dlqQueue, true, false, false, false, nil)
	failOnError(err, "Declare DLQ")
	failOnError(ch.QueueBind(dlqQueue, dlxRoutingKey, dlxExchange, false, nil), "Bind DLQ")

	failOnError(ch.Qos(1, 0, false), "Set QoS")

	// Start consuming main queue (autoAck = false)
	msgs, err := ch.Consume(queueName, "", false, false, false, false, nil)
	failOnError(err, "Consume main queue")

	// Consume messages from DLQ (autoAck = false, y hacemos Ack a mano)
	dlqMsgs, err := ch.Consume(dlqQueue, "", false, false, false, false, nil)
	failOnError(err, "Consume DLQ")

	fmt.Printf("\n┌─────────────────────┬────────────────┬─────────────────────────────────────┐\n")
	fmt.Printf("│ %-19s │ %-14s │ %-35s │\n", "Timestamp", "Status", "Message")
	fmt.Printf("├─────────────────────┼────────────────┼─────────────────────────────────────┤\n")

	// Main queue goroutine
	go func() {
		for d := range msgs {
			timestamp := time.Now().Format("15:04:05.000")
			message := string(d.Body)
			if rand.Intn(3) == 0 {
				d.Ack(false)
				printMessageStatus(MessageStatus{
					Timestamp:  timestamp,
					Status:     "SUCCESS",
					Message:    message,
					StatusType: "success",
				})
			} else {
				// NACK y requeue=true: vuelve a la cola, si no se procesa antes de TTL va a la DLQ
				d.Nack(false, true)
				printMessageStatus(MessageStatus{
					Timestamp:  timestamp,
					Status:     "FAIL (REQUEUE)",
					Message:    message,
					StatusType: "fail",
				})
			}
		}
	}()

	// DLQ goroutine
	go func() {
		for d := range dlqMsgs {
			timestamp := time.Now().Format("15:04:05.000")
			message := string(d.Body)
			printMessageStatus(MessageStatus{
				Timestamp:  timestamp,
				Status:     "TO DLQ",
				Message:    message,
				StatusType: "dlq",
			})
			// Hacemos Ack para que el mensaje se borre de la DLQ tras mostrarlo
			d.Ack(false)
		}
	}()

	select {}
}
