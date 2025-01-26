# **GoNotify – Event-Driven Notification System**

🚀 **GoNotify** is a lightweight, event-driven notification system built with **Golang** and **RabbitMQ**, designed to seamlessly integrate with any backend server. It enables reliable, asynchronous notification delivery across multiple channels, ensuring scalability and high performance.

## **Key Features**

✅ **Seamless Integration** – Works with any backend (REST, GraphQL, gRPC).\
✅ **Event-Driven Architecture** – Decoupled and scalable notification handling.\
✅ **RabbitMQ-Based Queuing** – Ensures reliable and asynchronous processing.\
✅ **Multi-Channel Support** – Easily extendable to Email, SMS, WebSockets, Push.\
✅ **Observability** – Built-in logging, metrics, and monitoring with Prometheus & Grafana.\
✅ **High Performance & Scalability** – Optimized for real-time event handling.

## **Tech Stack**

🔹 **Golang** – High-performance backend development\
🔹 **RabbitMQ** – Message broker for async event processing\
🔹 **Prometheus & Grafana** – Monitoring, logging, and observability\
🔹 **PostgreSQL / MongoDB** – Optional notification storage

## **Use Case**

Ideal for **e-commerce, SaaS, fintech, and microservices**, GoNotify enables real-time notifications for order updates, system alerts, and user engagement, ensuring a responsive and scalable event-driven architecture.

## **Design Decisions for GoNotify**

### Event-Driven Architecture

- **RabbitMQ** is chosen over **Kafka** for the following reasons:
  - **Message Acknowledgment & Delivery Guarantees**: RabbitMQ’s robust message acknowledgment mechanism ensures reliable message delivery.
  - **Routing & Fan-out Patterns**: RabbitMQ supports Direct Exchange, useful for routing notifications (email, SMS, push).
  - **Lower Throughput Requirement**: RabbitMQ is ideal for scenarios where the focus is on reliability over massive throughput.

### Tech Stack Decisions

#### gRPC vs REST

- **gRPC**: Chosen for internal communication between microservices, offering better performance and bi-directional streaming.
- **REST**: Used for communication with third-party services like **Twilio** and **SendGrid** for external notifications.
- **Decision**: Hybrid Approach — **gRPC** for internal calls, **REST** for external third-party integrations.

#### Message Broker: RabbitMQ

- **Queues**: Separate queues per notification type (email, SMS, push).
- **Exchanges**: Direct, Topic, and Fan-out exchanges are configured for routing notifications to appropriate channels.
- **Dead Letter Queue (DLQ)**: Implemented for retrying failed notifications.

#### Database: PostgreSQL + Redis

- **PostgreSQL**: Used for storing **notification logs**, offering ACID properties and relational capabilities.
- **Redis**: Utilized for **rate limiting**, ensuring notifications are not sent too frequently.

### Observability

- **Prometheus** and **Grafana** are used for **metrics and monitoring**, providing insights into system performance.
- **Loki** is used for **logging**, enabling efficient storage and querying of logs.

### Deployment

- **Docker** containers for consistent environments across development and production.
- **Kubernetes** for **orchestration** and scaling, ensuring the system can handle varying loads efficiently.

## **Prerequisites**

- Go 1.20+
- Docker
- Kubernetes (optional)
- RabbitMQ
- PostgreSQL
- Redis

## **Installation**

1. Clone this repository:

   ```bash
   git clone https://github.com/your-username/GoNotify.git
   cd GoNotify
   ```

2. Install dependencies:

   ```bash
   go mod download
   ```

3. Set up environment variables:

   ```bash
   cp .env.example .env
   # Edit .env with your configuration
   ```

4. Start services with Docker Compose:
   ```bash
   docker-compose up -d
   ```

## **License**

Distributed under the MIT License. See `LICENSE` for more information.
