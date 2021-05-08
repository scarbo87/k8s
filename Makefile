dep:
	go mod tidy -v
	go mod vendor

build: dep
	go build -o ./bin/k8s-example

docker-build:
	docker build -t k8s-example:latest .