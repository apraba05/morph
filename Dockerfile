FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod main.go index.html ./
RUN CGO_ENABLED=0 go build -o /router .

FROM scratch
COPY --from=build /router /router
EXPOSE 8000
ENTRYPOINT ["/router"]
