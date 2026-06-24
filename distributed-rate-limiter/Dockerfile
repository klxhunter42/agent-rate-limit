# Multi-stage Docker build for Distributed Rate Limiter
# Build stage (Maven pre-installed, avoids broken mvnw wrapper)
FROM maven:3.9-eclipse-temurin-21 AS build

# Set working directory
WORKDIR /app

# Build behind a corporate TLS-intercepting proxy without a committed CA:
# Maven wagon flags relax TLS verification for dependency downloads
# (mirrors api-gateway GOINSECURE/GIT_SSL_NO_VERIFY).

# Copy the entire project
COPY . .

# Build the application.
# Force the Wagon transport (Maven 3.9 defaults to Resolver, which ignores the
# wagon ssl flags) and relax TLS for dependency downloads behind a corporate
# TLS-intercepting proxy without a committed CA (mirrors api-gateway
# GOINSECURE/GIT_SSL_NO_VERIFY).
RUN mvn package -DskipTests -B \
 -Dmaven.resolver.transport=wagon \
 -Dmaven.wagon.http.ssl.insecure=true \
 -Dmaven.wagon.http.ssl.allowall=true \
 -Dmaven.wagon.http.ssl.ignore.validity.dates=true

# Runtime stage
FROM eclipse-temurin:21-jre AS runtime

# Install curl for health checks
RUN apt-get update && apt-get install -y curl && rm -rf /var/lib/apt/lists/*

# Create non-root user for security
RUN groupadd -g 1001 appuser && \
 useradd -d /app -g appuser -u 1001 appuser

# Set working directory
WORKDIR /app

# Copy the built JAR from build stage
COPY --from=build /app/target/*.jar app.jar

# Change ownership to non-root user
RUN chown -R appuser /app

# Switch to non-root user
USER appuser

# Expose port
EXPOSE 8080

# Configure JVM options for containerized environment
ENV JAVA_OPTS="-Djava.security.egd=file:/dev/./urandom -Xmx512m -Xms256m"

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
 CMD curl -f http://localhost:8080/actuator/health || exit 1

# Start the application with graceful shutdown
ENTRYPOINT ["sh", "-c", "java $JAVA_OPTS -jar app.jar"]
