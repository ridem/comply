package render

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/pkg/errors"
	"github.com/strongdm/comply/internal/config"
)

var pandocArgs = []string{"-f", "markdown+smart", "--toc", "-N", "--fail-if-warnings", "--pdf-engine=lualatex", "--template", "templates/default.latex", "-o"}

func pandoc(outputFilename string) error {
	if config.WhichPandoc() == config.UsePandoc {
		return pandocPandoc(outputFilename)
	}
	return dockerPandoc(outputFilename)
}

func dockerPandoc(outputFilename string) error {
	pandocCmd := append(pandocArgs, fmt.Sprintf("/source/output/%s", outputFilename), fmt.Sprintf("/source/output/%s.md", outputFilename))
	ctx := context.Background()
	cli, err := client.NewEnvClient()
	if err != nil {
		return errors.Wrap(err, "unable to read Docker environment")
	}

	pwd, err := os.Getwd()
	if err != nil {
		return errors.Wrap(err, "unable to get workding directory")
	}

	hc := &container.HostConfig{
		Binds: []string{pwd + ":/source"},
	}

	resp, err := cli.ContainerCreate(ctx, &container.Config{
		Image: "strongdm/pandoc",
		Cmd:   pandocCmd},
		hc, nil, "")

	if err != nil {
		return errors.Wrap(err, "unable to create Docker container")
	}

	defer func() error {
		timeout := 2 * time.Second
		cli.ContainerStop(ctx, resp.ID, &timeout)
		err := cli.ContainerRemove(ctx, resp.ID, types.ContainerRemoveOptions{Force: true})
		if err != nil {
			return errors.Wrap(err, "unable to remove container")
		}
		return nil
	}()

	if err := cli.ContainerStart(ctx, resp.ID, types.ContainerStartOptions{}); err != nil {
		return errors.Wrap(err, "unable to start Docker container")
	}

	_, err = cli.ContainerWait(ctx, resp.ID)
	if err != nil {
		return errors.Wrap(err, "error awaiting Docker container")
	}

	_, err = cli.ContainerLogs(ctx, resp.ID, types.ContainerLogsOptions{ShowStdout: true})
	if err != nil {
		return errors.Wrap(err, "error reading Docker container logs")
	}

	if _, err = os.Stat(fmt.Sprintf("output/%s", outputFilename)); err != nil && os.IsNotExist(err) {
		return errors.Wrap(err, "output not generated; verify your Docker image is up to date")
	}
	return nil
}

// 🐼
func pandocPandoc(outputFilename string) error {
	cmd := exec.Command("pandoc", append(pandocArgs, fmt.Sprintf("output/%s", outputFilename), fmt.Sprintf("output/%s.md", outputFilename))...)
	outputRaw, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Println(string(outputRaw))
		return errors.Wrap(err, "error calling pandoc")
	}
	return nil
}
