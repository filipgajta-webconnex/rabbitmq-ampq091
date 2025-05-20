package main

import (
	"fmt"
	"log"
	"math/rand"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	maxRetries    = 3
	exchangeName  = "example_exchange"
	routingKey    = "example_key"
	queueName     = "example_queue"
	dlxExchange   = "dlx_exchange"
	dlxRoutingKey = "dlx_key"
	dlqQueue      = "dlq_queue"
)

func main() {
	// Connect to RabbitMQ
	conn, err := amqp.Dial("amqp://guest:guest@localhost:5672/")
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	defer conn.Close()

	// Open a channel
	ch, err := conn.Channel()
	if err != nil {
		log.Fatalf("Failed to open a channel: %v", err)
	}
	defer ch.Close()

	// Declare the Dead Letter Exchange (DLX)
	err = ch.ExchangeDeclare(
		dlxExchange, // name
		"direct",    // type
		true,        // durable
		false,       // auto-deleted
		false,       // internal
		false,       // no-wait
		nil,         // arguments
	)
	if err != nil {
		log.Fatalf("Failed to declare DLX: %v", err)
	}

	// Declare the Dead Letter Queue (DLQ)
	_, err = ch.QueueDeclare(
		dlqQueue, // name
		true,     // durable
		false,    // delete when unused
		false,    // exclusive
		false,    // no-wait
		nil,      // arguments
	)
	if err != nil {
		log.Fatalf("Failed to declare DLQ: %v", err)
	}

	// Bind the DLQ to the DLX
	err = ch.QueueBind(
		dlqQueue,      // queue name
		dlxRoutingKey, // routing key
		dlxExchange,   // exchange
		false,         // no-wait
		nil,           // arguments
	)
	if err != nil {
		log.Fatalf("Failed to bind DLQ: %v", err)
	}

	// Declare the main exchange
	err = ch.ExchangeDeclare(
		exchangeName, // name
		"direct",     // type
		true,         // durable
		false,        // auto-deleted
		false,        // internal
		false,        // no-wait
		nil,          // arguments
	)
	if err != nil {
		log.Fatalf("Failed to declare exchange: %v", err)
	}

	// Declare the main queue with DLQ settings
	args := amqp.Table{
		"x-dead-letter-exchange":    dlxExchange,   // Dead Letter Exchange
		"x-dead-letter-routing-key": dlxRoutingKey, // Routing key for DLQ
	}
	_, err = ch.QueueDeclare(
		queueName, // name
		true,      // durable
		false,     // delete when unused
		false,     // exclusive
		false,     // no-wait
		args,      // arguments
	)
	if err != nil {
		log.Fatalf("Failed to declare a queue: %v", err)
	}

	// Bind the queue to the exchange
	err = ch.QueueBind(
		queueName,    // queue name
		routingKey,   // routing key
		exchangeName, // exchange
		false,        // no-wait
		nil,          // arguments
	)
	if err != nil {
		log.Fatalf("Failed to bind queue: %v", err)
	}

	// Set QoS (Quality of Service)
	err = ch.Qos(1, 0, false) // Prefetch 1 message at a time
	if err != nil {
		log.Fatalf("Failed to set QoS: %v", err)
	}

	// Consume messages
	msgs, err := ch.Consume(
		queueName, // queue
		"",        // consumer
		false,     // auto-ack
		false,     // exclusive
		false,     // no-local
		false,     // no-wait
		nil,       // args
	)
	if err != nil {
		log.Fatalf("Failed to register a consumer: %v", err)
	}

	// Process messages
	forever := make(chan bool)

	// Print table header
	fmt.Printf("\n┌─────────────────────┬────────────────┬─────────────────────────────────────┐\n")
	fmt.Printf("│ %-19s │ %-14s │ %-35s │\n", "Timestamp", "Status", "Message")
	fmt.Printf("├─────────────────────┼────────────────┼─────────────────────────────────────┤\n")

	go func() {
		for d := range msgs {
			timestamp := time.Now().Format("15:04:05.000")
			message := string(d.Body)

			// Get retry count from headers or initialize it
			headers := d.Headers
			if headers == nil {
				headers = make(amqp.Table)
			}
			retryCount, _ := headers["retry-count"].(int32)

			if rand.Intn(2) == 0 {
				fmt.Printf("│ \033[90m%-19s\033[0m │ \033[32m%-14s\033[0m │ %-35s │\n",
					timestamp,
					"SUCCESS",
					message)
				d.Ack(false)
			} else {
				retryCount++
				if retryCount >= maxRetries {
					fmt.Printf("│ \033[90m%-19s\033[0m │ \033[31m%-14s\033[0m │ %-35s │\n",
						timestamp,
						"TO DLQ",
						message)
					// Reject without requeue - will go to DLQ
					d.Nack(false, false)
				} else {
					fmt.Printf("│ \033[90m%-19s\033[0m │ \033[33m%-14s\033[0m │ %-35s │\n",
						timestamp,
						fmt.Sprintf("RETRY %d/%d", retryCount, maxRetries),
						message)
					// Update retry count in headers
					headers["retry-count"] = retryCount
					// Republish with updated headers
					err := ch.Publish(
						exchangeName,
						routingKey,
						false,
						false,
						amqp.Publishing{
							Headers:     headers,
							Body:        d.Body,
							ContentType: "text/plain",
						})
					if err != nil {
						log.Printf("Failed to republish message: %v", err)
						d.Nack(false, true) // Requeue on publish failure
						continue
					}
					d.Ack(false)
				}
			}
			fmt.Printf("├─────────────────────┼────────────────┼─────────────────────────────────────┤\n")
		}
	}()

	<-forever
}
