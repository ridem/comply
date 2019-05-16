package cli

import (
	"fmt"

	"github.com/ridem/comply/internal/config"
	"github.com/ridem/comply/internal/model"
	"github.com/urfave/cli"
)

var procedureCommand = cli.Command{
	Name:      "procedure",
	ShortName: "proc",
	Usage:     "create ticket by procedure ID",
	ArgsUsage: "procedureID",
	Action:    procedureAction,
	Before:    beforeAll(projectMustExist, ticketingMustBeConfigured),
}

func procedureAction(c *cli.Context) error {
	procedures, err := model.ReadProcedures()
	if err != nil {
		return err
	}

	if c.NArg() != 1 {
		return cli.NewExitError("provide a procedure ID", 1)
	}

	procedureID := c.Args().First()

	ts, err := config.Config().TicketSystem()
	if err != nil {
		return cli.NewExitError("error in ticket system configuration", 1)
	}

	tp := model.GetPlugin(model.TicketSystem(ts))

	for _, procedure := range procedures {
		if procedure.ID == procedureID {
			err = tp.Create(&model.Ticket{
				Name: procedure.Name,
				Body: fmt.Sprintf("%s\n\n\n---\nProcedure-ID: %s", procedure.Body, procedure.ID),
			}, []string{"comply", "comply-procedure"})
			if err != nil {
				return err
			}
			return nil
		}
	}

	return cli.NewExitError(fmt.Sprintf("unknown procedure ID: %s", procedureID), 1)
}
