module github.com/paryty/paryty-v1.0/tests/load

go 1.22

require (
	github.com/paryty/paryty-v1.0/cluster v0.0.0
	google.golang.org/grpc v1.64.0
	gopkg.in/yaml.v3 v3.0.1
)

replace github.com/paryty/paryty-v1.0/cluster => ../../cluster
