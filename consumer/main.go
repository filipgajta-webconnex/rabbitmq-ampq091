package main

import (
	"fmt"
	"log"
	"math/rand"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	maxRetries      = 3
	exchangeName    = "example_exchange"
	retryExchange   = "retry_exchange"
	dlxExchange     = "dlx_exchange"
	queueName       = "example_queue"
	retryQueue      = "retry_queue"
	dlqQueue        = "dlq_queue"
	routingKey      = "example_key"
	retryRoutingKey = "retry_key"
	dlxRoutingKey   = "dlq_key"
	retryTTL        = 2000 // milliseconds (2 seconds)
)

// MessageStatus for colored output
type MessageStatus struct {
	Timestamp  string
	Status     string
	Message    string
	StatusType string // "success", "retry", "dlq"
}

func printMessageStatus(status MessageStatus) {
	var colorCode string
	switch status.StatusType {
	case "success":
		colorCode = "\033[32m"
	case "retry":
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

func getRetryCount(headers amqp.Table) int64 {
	if headers == nil {
		return 0
	}
	xDeath, ok := headers["x-death"].([]interface{})
	if !ok || len(xDeath) == 0 {
		return 0
	}
	deathInfo, ok := xDeath[0].(amqp.Table)
	if !ok {
		return 0
	}
	count, ok := deathInfo["count"].(int64)
	if !ok {
		return 0
	}
	return count
}

func handleMessage(d amqp.Delivery) MessageStatus {
	timestamp := time.Now().Format("15:04:05.000")
	message := string(d.Body)
	retryCount := getRetryCount(d.Headers)

	// Simulate random failure (50% chance)
	if rand.Intn(2) == 0 {
		d.Ack(false)
		return MessageStatus{
			Timestamp:  timestamp,
			Status:     "SUCCESS",
			Message:    message,
			StatusType: "success",
		}
	}

	retryCount++
	if retryCount > maxRetries {
		d.Nack(false, false) // Send to DLQ
		return MessageStatus{
			Timestamp:  timestamp,
			Status:     "TO DLQ",
			Message:    message,
			StatusType: "dlq",
		}
	}

	d.Nack(false, false) // Trigger retry via DLX
	return MessageStatus{
		Timestamp:  timestamp,
		Status:     fmt.Sprintf("RETRY %d/%d", retryCount, maxRetries),
		Message:    message,
		StatusType: "retry",
	}
}

func main() {
	conn, err := amqp.Dial("amqp://guest:guest@localhost:5672/")
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		log.Fatalf("Failed to open a channel: %v", err)
	}
	defer ch.Close()

	// Main exchange
	if err := ch.ExchangeDeclare(exchangeName, "topic", true, false, false, false, nil); err != nil {
		log.Fatalf("Failed to declare main exchange: %v", err)
	}
	// Retry exchange
	if err := ch.ExchangeDeclare(retryExchange, "topic", true, false, false, false, nil); err != nil {
		log.Fatalf("Failed to declare retry exchange: %v", err)
	}
	// DLX exchange
	if err := ch.ExchangeDeclare(dlxExchange, "topic", true, false, false, false, nil); err != nil {
		log.Fatalf("Failed to declare DLX exchange: %v", err)
	}

	// Main queue (dead-letters to retry exchange)
	mainArgs := amqp.Table{
		"x-dead-letter-exchange":    retryExchange,
		"x-dead-letter-routing-key": retryRoutingKey,
	}
	_, err = ch.QueueDeclare(queueName, true, false, false, false, mainArgs)
	if err != nil {
		log.Fatalf("Failed to declare main queue: %v", err)
	}
	if err := ch.QueueBind(queueName, routingKey, exchangeName, false, nil); err != nil {
		log.Fatalf("Failed to bind main queue: %v", err)
	}

	// Retry queue: TTL, then dead-letters back to main exchange
	retryArgs := amqp.Table{
		"x-dead-letter-exchange":    exchangeName,
		"x-dead-letter-routing-key": routingKey,
		"x-message-ttl":             int32(retryTTL),
	}
	_, err = ch.QueueDeclare(retryQueue, true, false, false, false, retryArgs)
	if err != nil {
		log.Fatalf("Failed to declare retry queue: %v", err)
	}
	if err := ch.QueueBind(retryQueue, retryRoutingKey, retryExchange, false, nil); err != nil {
		log.Fatalf("Failed to bind retry queue: %v", err)
	}

	// DLQ: final destination for dead-lettered messages
	_, err = ch.QueueDeclare(dlqQueue, true, false, false, false, nil)
	if err != nil {
		log.Fatalf("Failed to declare DLQ: %v", err)
	}
	if err := ch.QueueBind(dlqQueue, dlxRoutingKey, dlxExchange, false, nil); err != nil {
		log.Fatalf("Failed to bind DLQ: %v", err)
	}

	// Set QoS
	err = ch.Qos(1, 0, false)
	if err != nil {
		log.Fatalf("Failed to set QoS: %v", err)
	}

	// Start consuming
	msgs, err := ch.Consume(queueName, "", false, false, false, false, nil)
	if err != nil {
		log.Fatalf("Failed to register a consumer: %v", err)
	}

	fmt.Printf("\n┌─────────────────────┬────────────────┬─────────────────────────────────────┐\n")
	fmt.Printf("│ %-19s │ %-14s │ %-35s │\n", "Timestamp", "Status", "Message")
	fmt.Printf("├─────────────────────┼────────────────┼─────────────────────────────────────┤\n")

	go func() {
		for d := range msgs {
			status := handleMessage(d)
			printMessageStatus(status)
		}
	}()

	select {}
}
