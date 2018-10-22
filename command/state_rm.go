package command

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/mitchellh/cli"

	"github.com/hashicorp/terraform/addrs"
	"github.com/hashicorp/terraform/states"
)

// StateRmCommand is a Command implementation that shows a single resource.
type StateRmCommand struct {
	StateMeta
}

func (c *StateRmCommand) Run(args []string) int {
	args, err := c.Meta.process(args, true)
	if err != nil {
		return 1
	}

	cmdFlags := c.Meta.flagSet("state show")
	cmdFlags.StringVar(&c.backupPath, "backup", "-", "backup")
	cmdFlags.StringVar(&c.statePath, "state", "", "path")
	dryRun := cmdFlags.Bool("dry-run", false, "dry run")
	if err := cmdFlags.Parse(args); err != nil {
		return cli.RunResultHelp
	}
	args = cmdFlags.Args()

	if len(args) < 1 {
		c.Ui.Error("At least one resource address is required.")
		return 1
	}

	// Get the state
	stateMgr, err := c.State()
	if err != nil {
		c.Ui.Error(fmt.Sprintf(errStateLoadingState, err))
		return 1
	}
	if err := stateMgr.RefreshState(); err != nil {
		c.Ui.Error(fmt.Sprintf("Failed to refresh state: %s", err))
		return 1
	}

	state := stateMgr.State()
	if state == nil {
		c.Ui.Error(fmt.Sprintf(errStateNotFound))
		return 1
	}

	var results []*states.FilterResult
	filter := &states.Filter{State: state}
	for _, arg := range args {
		filtered, err := filter.Filter(arg)
		if err != nil {
			c.Ui.Error(fmt.Sprintf(errStateFilter, err))
			return cli.RunResultHelp
		}
	filtered:
		for _, result := range filtered {
			switch result.Address.(type) {
			case addrs.ModuleInstance:
				for _, result := range filtered {
					if _, ok := result.Address.(addrs.ModuleInstance); ok {
						results = append(results, result)
					}
				}
				break filtered
			case addrs.AbsResource:
				for _, result := range filtered {
					if _, ok := result.Address.(addrs.AbsResource); ok {
						results = append(results, result)
					}
				}
				break filtered
			case addrs.AbsResourceInstance:
				results = append(results, result)
			}
		}
	}

	// If we are in dry run mode, print out what we would've done.
	if *dryRun {
		dryRunBuf := bytes.NewBuffer(nil)
		for _, result := range results {
			switch addr := result.Address.(type) {
			case addrs.ModuleInstance:
				output := " containing:\n"
				for _, rs := range result.Value.(*states.Module).Resources {
					for k := range rs.Instances {
						output += fmt.Sprintf("  * %s\n", rs.Addr.Absolute(addr).Instance(k))
					}
				}
				if output == " containing:\n" {
					output = "\n"
				}
				fmt.Fprintf(dryRunBuf, "Would remove module %q%s", addr.String(), output)
			case addrs.AbsResource:
				fmt.Fprintf(dryRunBuf, "Would remove resource %s\n", addr.String())
			case addrs.AbsResourceInstance:
				fmt.Fprintf(dryRunBuf, "Would remove resource instance %s\n", addr.String())
			}
		}

		result := fmt.Sprintf(strings.TrimSuffix(dryRunBuf.String(), "\n"))
		if result == "" {
			result = "Would have removed nothing."
		}
		c.Ui.Output(result)
		return 0 // This is as far as we go in dry-run mode
	}

	// Now we will actually remove them. We'll use the "SyncState" wrapper to
	// do this not because we're doing any concurrent work here (we aren't) but
	// because it guarantees to clean up any leftover empty module we might
	// leave behind.
	ss := state.SyncWrapper()
	for _, result := range results {
		switch addr := result.Address.(type) {
		case addrs.ModuleInstance:
			c.Ui.Output(fmt.Sprintf("Remove module %s", addr.String()))
			ss.RemoveModule(addr)
		case addrs.AbsResource:
			c.Ui.Output(fmt.Sprintf("Remove resource %s", addr.String()))
			ss.RemoveResource(addr)
		case addrs.AbsResourceInstance:
			c.Ui.Output(fmt.Sprintf("Remove resource instance %s", addr.String()))
			ss.ForgetResourceInstanceAll(addr)
		}
	}

	if err := stateMgr.WriteState(state); err != nil {
		c.Ui.Error(fmt.Sprintf(errStateRmPersist, err))
		return 1
	}
	if err := stateMgr.PersistState(); err != nil {
		c.Ui.Error(fmt.Sprintf(errStateRmPersist, err))
		return 1
	}

	c.Ui.Output("Updated state written successfully.")
	return 0
}

func (c *StateRmCommand) Help() string {
	helpText := `
Usage: terraform state rm [options] ADDRESS...

  Remove one or more items from the Terraform state.

  This command removes one or more resource instances from the Terraform state
  based on the addresses given. You can view and list the available instances
  with "terraform state list".

  This command creates a timestamped backup of the state on every invocation.
  This can't be disabled. Due to the destructive nature of this command,
  the backup is ensured by Terraform for safety reasons.

Options:

  -dry-run            If set, prints out what would've been removed but
                      doesn't actually remove anything.

  -backup=PATH        Path where Terraform should write the backup
                      state. This can't be disabled. If not set, Terraform
                      will write it to the same path as the statefile with
                      a backup extension.

  -state=PATH         Path to the source state file. Defaults to the configured
                      backend, or "terraform.tfstate"

`
	return strings.TrimSpace(helpText)
}

func (c *StateRmCommand) Synopsis() string {
	return "Remove instances from the state"
}

const errStateRm = `Error removing items from the state: %s

The state was not saved. No items were removed from the persisted
state. No backup was created since no modification occurred. Please
resolve the issue above and try again.`

const errStateRmPersist = `Error saving the state: %s

The state was not saved. No items were removed from the persisted
state. No backup was created since no modification occurred. Please
resolve the issue above and try again.`
