package main

import (
	"context"
	"log"
	"os"

	"github.com/kgateway-dev/kgateway/v2/test/envtestutil"
)

func main() {
	ctx := context.Background()
	dumper := envtestutil.NewXdsDumper(nil, ctx, envtestutil.WithXdsPort(9977), envtestutil.WithGwns("common-infrastructure"), envtestutil.WithGwname("egress-gateway"))
	defer dumper.Close()

	dump, err := dumper.Dump(nil, ctx)
	if err != nil {
		log.Fatalf("failed to dump xds: %v", err)
	}

	yaml, err := dump.ToYaml()
	if err != nil {
		log.Fatalf("failed to convert xds dump to yaml: %v", err)
	}

	os.Stdout.Write(yaml)
}
