package cli

import (
	"fmt"
	"io"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/logrusorgru/aurora"
	"github.com/spf13/cobra"

	"replicate.ai/cli/pkg/console"
	"replicate.ai/cli/pkg/param"
	"replicate.ai/cli/pkg/project"
)

func newDiffCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff <ID> <ID>",
		Short: "Compare two experiments or checkpoints",
		Long: `Compare two experiments or checkpoints.

If an experiment ID is passed, it will pick the best checkpoint from that experiment. If a primary metric is not defined in replicate.yaml, it will use the latest checkpoint.`,
		RunE: diffCheckpoints,
		Args: cobra.ExactArgs(2),
	}

	// TODO(andreas): support json output
	addStorageURLFlag(cmd)

	return cmd
}

func diffCheckpoints(cmd *cobra.Command, args []string) error {
	// TODO(andreas): generalize to >2 checkpoints/experiments

	prefix1 := args[0]
	prefix2 := args[1]

	storageURL, sourceDir, err := getStorageURLFromFlagOrConfig(cmd)
	if err != nil {
		return err
	}
	store, err := getStorage(storageURL, sourceDir)
	if err != nil {
		return err
	}
	proj := project.NewProject(store)
	au := getAurora()
	return printDiff(os.Stdout, au, proj, prefix1, prefix2)
}

// TODO: implement this as a thing in console
func br(w *tabwriter.Writer) {
	fmt.Fprintf(w, "\t\t\n")
}

func heading(w *tabwriter.Writer, au aurora.Aurora, text string) {
	fmt.Fprintf(w, "%s\t\t\n", au.Bold(text))
}

// TODO(andreas): diff command line arguments
func printDiff(out io.Writer, au aurora.Aurora, proj *project.Project, prefix1 string, prefix2 string) error {
	com1, err := loadCheckpoint(proj, prefix1)
	if err != nil {
		return err
	}
	com2, err := loadCheckpoint(proj, prefix2)
	if err != nil {
		return err
	}
	exp1, err := proj.ExperimentByID(com1.ExperimentID)
	if err != nil {
		return err
	}
	exp2, err := proj.ExperimentByID(com2.ExperimentID)
	if err != nil {
		return err
	}

	// min width for 3 columns in 78 char terminal
	w := tabwriter.NewWriter(out, 78/3, 8, 2, ' ', 0)

	fmt.Fprintf(w, "Checkpoint:\t%s\t%s\n", com1.ShortID(), com2.ShortID())
	fmt.Fprintf(w, "Experiment:\t%s\t%s\n", com1.ShortExperimentID(), com2.ShortExperimentID())

	br(w)
	heading(w, au, "Params")
	printMapDiff(w, au, paramMapToStringMap(exp1.Params), paramMapToStringMap(exp2.Params))
	br(w)

	heading(w, au, "Metrics")
	// TODO(bfirsh): put primary metric first
	printMapDiff(w, au, paramMapToStringMap(com1.Metrics), paramMapToStringMap(com2.Metrics))
	br(w)

	return w.Flush()
}

func printMapDiff(w *tabwriter.Writer, au aurora.Aurora, map1, map2 map[string]string) {
	diffMap := mapString(map1, map2)

	// sort the keys
	type keyVal struct {
		key   string
		value []*string
	}
	keyVals := []keyVal{}
	for k, v := range diffMap {
		keyVals = append(keyVals, keyVal{k, v})
	}
	sort.Slice(keyVals, func(i, j int) bool {
		return keyVals[i].key < keyVals[j].key
	})

	if len(keyVals) > 0 {
		for _, kv := range keyVals {
			left := "(not set)"
			right := "(not set)"
			if kv.value[0] != nil {
				left = *(kv.value[0])
			}
			if kv.value[1] != nil {
				right = *(kv.value[1])
			}
			fmt.Fprintf(w, "%s:\t%s\t%s\n", kv.key, left, right)
		}
	} else {
		fmt.Fprintf(w, "%s\t\t\n", au.Faint("(no difference)"))
	}
}

func paramMapToStringMap(params map[string]*param.Value) map[string]string {
	result := make(map[string]string)
	for k, v := range params {
		result[k] = v.String()
	}
	return result
}

// loadCheckpoint returns a checkpoint given a prefix. If the prefix matches a
// checkpoint, that is returned. If the prefix matches an experiment, it
// returns the best checkpoint if a primary metric is defined in config,
// otherwise the latest checkpoint.
func loadCheckpoint(proj *project.Project, prefix string) (*project.Checkpoint, error) {
	obj, err := proj.CheckpointOrExperimentFromPrefix(prefix)
	if err != nil {
		return nil, err
	}
	if obj.Checkpoint != nil {
		return obj.Checkpoint, nil
	}
	exp := obj.Experiment

	// First, try getting best checkpoint
	checkpoint, err := proj.ExperimentBestCheckpoint(exp.ID)
	if err != nil {
		return nil, err
	}
	if checkpoint != nil {
		console.Info("%q matches an experiment, picking the best checkpoint", prefix)
		return checkpoint, nil
	}

	// If there is no best checkpoint and no error, then no primary metric has been set,
	// so fall back to picking latest checkpoint
	console.Info("%q is an experiment, picking the latest checkpoint", prefix)
	checkpoint, err = proj.ExperimentLatestCheckpoint(exp.ID)
	if err != nil {
		return nil, err
	}
	if checkpoint == nil {
		return nil, fmt.Errorf("Could not pick best checkpoint for experiment %q: it does not have any checkpoints.", exp.ShortID())
	}
	return checkpoint, nil
}

// mapString takes two maps of strings and returns a single map with two values
// where the values are different. If only one map has a key, then the map
// without the value will be marked as nil
//
// e.g.
// >>> mapString({"layers": "2", "foo": "bar"}, {"layers": "4"})
// {
//    "foo": ["bar", nil],
//	  "layers": ["2", "4"]
// }
//
func mapString(left, right map[string]string) map[string][]*string {
	result := make(map[string][]*string)
	for k, v := range left {
		if _, ok := right[k]; ok {
			// Key in both left and right
			if v != right[k] {
				// copy so pointers are unique
				v2 := v
				s := right[k]
				result[k] = []*string{&v2, &s}
			}
		} else {
			// Key just in left
			v2 := v
			result[k] = []*string{&v2, nil}
		}
	}
	for k, v := range right {
		// Key just in right
		if _, ok := left[k]; !ok {
			v2 := v
			result[k] = []*string{nil, &v2}
		}
	}
	return result
}
