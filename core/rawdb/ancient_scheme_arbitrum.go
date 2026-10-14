package rawdb

import "fmt"

// SetChainFreezerTableOptional has to run before any chain freezer opens,
// because the table configs are a global map.
func SetChainFreezerTableOptional(name string) error {
	config, ok := chainFreezerTableConfigs[name]
	if !ok {
		return fmt.Errorf("unknown chain freezer table %q", name)
	}
	// A reset gives the table a nonzero tail, which repair rejects for a
	// non-prunable table and applies to every table sharing its group.
	if config.tailGroup == "" {
		return fmt.Errorf("chain freezer table %q is not prunable, so it can't be optional", name)
	}
	for other, cfg := range chainFreezerTableConfigs {
		if other != name && cfg.tailGroup == config.tailGroup {
			return fmt.Errorf("chain freezer table %q shares its tail group, so it can't be optional", name)
		}
	}
	config.optional = true
	chainFreezerTableConfigs[name] = config
	return nil
}
