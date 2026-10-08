package test

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

type workflowContract struct {
	On map[string]struct {
		Inputs map[string]struct {
			Required bool
			Type     string
		}
	}
	Jobs map[string]workflowJob
}

type workflowJob struct {
	Uses            string
	With            map[string]string
	Needs           yaml.Node
	If              string
	ContinueOnError yaml.Node `yaml:"continue-on-error"`
	Permissions     map[string]string
	RunsOn          string `yaml:"runs-on"`
	Strategy        struct {
		Matrix map[string][]string
	}
	Steps []workflowStep
}

type workflowStep struct {
	Uses            string
	Run             string
	With            map[string]string
	If              string
	ContinueOnError yaml.Node `yaml:"continue-on-error"`
}

func readWorkflowContract(t *testing.T, name string) workflowContract {
	t.Helper()
	var workflow workflowContract
	if err := yaml.Unmarshal([]byte(readRepoFile(t, name)), &workflow); err != nil {
		t.Fatal(err)
	}
	return workflow
}

func jobNeeds(t *testing.T, job workflowJob) []string {
	t.Helper()
	if job.Needs.Kind == 0 {
		return nil
	}
	if job.Needs.Kind == yaml.ScalarNode {
		return []string{job.Needs.Value}
	}
	var needs []string
	if err := job.Needs.Decode(&needs); err != nil {
		t.Fatal(err)
	}
	return needs
}

func assertRequiredJob(t *testing.T, name string, job workflowJob) {
	t.Helper()
	if job.If != "" || job.ContinueOnError.Kind != 0 {
		t.Errorf("%s must use the default success gate without optional job failures", name)
	}
	for _, step := range job.Steps {
		if step.If != "" || step.ContinueOnError.Kind != 0 {
			t.Errorf("%s step %q must not skip or ignore required verification/publication", name, step.Uses+step.Run)
		}
	}
}

func assertCheckoutCommit(t *testing.T, name string, job workflowJob, ref string) {
	t.Helper()
	found := false
	for _, step := range job.Steps {
		if strings.HasPrefix(step.Uses, "actions/checkout@") {
			found = true
			if step.With["ref"] != ref || step.With["persist-credentials"] != "false" {
				t.Errorf("%s checkout must explicitly use %s without persisted credentials", name, ref)
			}
		}
	}
	if !found {
		t.Errorf("%s has no commit-bound checkout", name)
	}
}
