# syntax=docker/dockerfile:1

# Build stage
FROM golang:1.25-alpine AS build

WORKDIR /app

# Copy go.mod / go.sum first for better layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY cmd/ cmd/
COPY pkg/ pkg/
COPY agent/ agent/
RUN CGO_ENABLED=0 GOOS=linux go build -o /vibesdk-backend ./cmd

# Runtime stage (distroless — no shell, no need for Docker inside)
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

COPY --from=build /vibesdk-backend /app/vibesdk-backend

# Per-role skill prompts (planner/coder system prompts). The app
# resolves them relative to WORKDIR (/app/skills) or via SKILLS_DIR.
COPY skills/ /app/skills/

# The backend listens on :8080
EXPOSE 8080

ENTRYPOINT ["/app/vibesdk-backend"]