# Contributing

Thank you for your interest in contributing to **go-notify**! We welcome contributions from the community.
Please review the following guidelines to help you get started.

## **Table of Contents**

- [Getting Started](#getting-started)
- [Local Development Setup](#local-development-setup)
- [Docker Setup](#docker-setup)
- [Running Tests](#running-tests)
- [Code Style](#code-style)
- [Pull Request Process](#pull-request-process)
- [Reporting Issues](#reporting-issues)
- [License](#license)

## **Getting Started**

Before you begin, please ensure that you have read our [Code of Conduct](CODE_OF_CONDUCT.md) and that you understand our contribution workflow.
To start:

1. **Fork** the repository.

2. **Clone** your fork locally:

   ```bash
   git clone https://github.com/yourusername/go-notify.git
   cd go-notify
   ```

## **Local Development Setup**

### **Prerequisites**

- [Go](https://golang.org/dl/) (version 1.22 or later is recommended)
- Git

1. **Install Dependencies**

   This project leverages Go modules. Download all required dependencies with:

   ```bash
   go mod download
   ```

2. **Build the Application**

   Build the executable locally:

   ```bash
   go build -o bin/go-notify .
   ```

3. **Run the Application**

   Execute the binary:

   ```bash
   ./bin/go-notify
   ```

4. **Development Workflow**

   - Create a new branch for your feature or bug fix:

     ```bash
     git checkout -b feature/your-feature-name
     ```

   - Make your changes and test locally.

   - Run linters to ensure code quality.

     ```bash
     make lint
     ```

## **Docker Setup**

This project provides a Docker configuration to simplify the setup and testing process.

### **Prerequisites**

- [Docker](https://docs.docker.com/get-docker/)

1. **Building the Docker Image**

   Create the Docker image by running:

   ```bash
   make docker-build
   ```

2. **Running the Docker Container**

   After building the image, start a container and map the necessary ports:

   ```bash
   make docker-up
   ```

## **Running Tests**

Before opening a pull request, please ensure that all tests pass:

```bash
make test
```

If any tests fail, address the issues before submitting your contribution.

## **Code Style**

- Follow Go’s official Effective Go guidelines.

- Write clear and descriptive commit messages.

- Maintain consistency in code formatting.

- Use linters to identify potential issues:
  ```bash
  make lint
  ```

## **Pull Request Process**

1. **Fork the repository and create your feature branch:**

   ```bash
   git checkout -b feature/your-feature-name
   ```

2. **Commit your changes with a descriptive message.**

3. **Push your branch to your fork:**

   ```bash
   git push origin feature/your-feature-name
   ```

4. **Open a pull request against the main repository with a detailed description of your changes.**

5. **Participate in code review and make any necessary adjustments.**

## **Reporting Issues**

If you encounter a bug or have a suggestion for an improvement, please help us improve go-notify by opening an issue on our [GitHub Issues](https://github.com/officiallysidsingh/go-notify/issues) page. When creating an issue, try to include:

- A clear description of the problem or enhancement
- Steps to reproduce the issue (if applicable)
- Any relevant error messages or logs
- Information about your environment (OS, Go version, etc.)

This information helps us diagnose and address problems more efficiently.

## **License**

All contributions to **go-notify** are made available under the terms of the project's [LICENSE](LICENSE). By contributing, you agree that your contributions will be distributed under this license. Please review the license file for more details.
