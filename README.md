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
🔹 **PostgreSQL** – Notification status storage\
🔹 **Redis** – Rate limiting notifications

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

## Architecture Diagram

![Architecture Diagram](https://github.com/user-attachments/assets/8858dd74-74e3-4189-a366-23c6924026cf)

## **Prerequisites**

- [Docker](https://www.docker.com/get-started)
- [Docker Compose](https://docs.docker.com/compose/)

## **Installation**

1. **Clone this repository:**

   ```bash
   git clone https://github.com/your-username/go-notify.git
   cd go-notify
   ```

2. **Configure the environment:**
   Update the configuration in config/config.yaml as needed.
   You can also refer to config/config.example.yaml for environment variable settings.

3. **Run the services using Docker Compose:**

   ```bash
   make docker-up
   ```

   This command will start RabbitMQ, PostgreSQL, gRPC server, worker, Prometheus, Grafana, and Loki.

4. **Access the Services:**

   - gRPC Server: localhost:50051
   - Prometheus Metrics: localhost:9090/metrics
   - Grafana Dashboard: localhost:3000 (default login: admin/admin)
   - RabbitMQ UI: localhost:15672

## **Testing**

- **Run tests locally:**

  ```bash
  make test
  ```

- **Generate Protobuf files:**

  ```bash
  make proto
  ```

## **CI/CD**

This repository uses GitHub Actions for continuous integration and delivery. The workflow is defined in `/.github/workflows/ci-cd.yml` and covers:

- **Linting**
- **Testing**
- **Building**
- **Docker image creation**

## **License**

Distributed under the MIT License. See `LICENSE` for more information.

## **Contributing**

Contributions are welcome! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.
