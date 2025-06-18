# RabbitMQ Message Processing Example

## Running the System

1. First, start RabbitMQ using Docker Compose:
```bash
docker-compose up -d
```

This will start RabbitMQ on port 5672 (AMQP) and the management interface on port 15672.
You can access the RabbitMQ management interface at http://localhost:15672 (default credentials: guest/guest)

2. Run the Consumer (in a separate terminal):
```bash
cd consumer
go run main.go
```

The consumer will start and display a table showing the processing status of each message.

3. Run the Producer (in another terminal):
```bash
cd producer
go run main.go
```

The producer will start sending messages to RabbitMQ, which will be processed by the consumer.

## Message Processing Behavior

- Messages are processed with a retry mechanism
- After maximum retries (3), failed messages are sent to a Dead Letter Queue (DLQ)
- The consumer displays real-time processing status with color-coded output:
  - 🟢 Green: Successfully processed messages
  - 🟡 Yellow: Messages being retried
  - 🔴 Red: Messages sent to DLQ

<img width="2649" alt="image" src="https://github.com/user-attachments/assets/ff187bf3-ede9-48a2-9ceb-b5744cc44d0a" />

## Stopping the System

1. Stop the producer and consumer by pressing Ctrl+C in their respective terminals
2. Stop RabbitMQ:
```bash
docker-compose down
```
