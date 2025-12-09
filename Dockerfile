FROM golang:1.21-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o extractor

FROM scratch
COPY --from=build /app/extractor /extractor
EXPOSE 8080
CMD ["/extractor"]
