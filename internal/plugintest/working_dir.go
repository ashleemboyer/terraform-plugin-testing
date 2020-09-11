package plugintest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/hashicorp/terraform-exec/tfexec"
	tfjson "github.com/hashicorp/terraform-json"
)

// WorkingDir represents a distinct working directory that can be used for
// running tests. Each test should construct its own WorkingDir by calling
// NewWorkingDir or RequireNewWorkingDir on its package's singleton
// plugintest.Helper.
type WorkingDir struct {
	h *Helper

	// baseDir is the root of the working directory tree
	baseDir string

	// baseArgs is arguments that should be appended to all commands
	baseArgs []string

	// configDir contains the singular config file generated for each test
	configDir string

	// tf is the instance of tfexec.Terraform used for running Terraform commands
	tf *tfexec.Terraform

	// terraformExec is a path to a terraform binary, inherited from Helper
	terraformExec string

	// reattachInfo stores the gRPC socket info required for Terraform's
	// plugin reattach functionality
	reattachInfo tfexec.ReattachInfo

	env map[string]string
}

// Close deletes the directories and files created to represent the receiving
// working directory. After this method is called, the working directory object
// is invalid and may no longer be used.
func (wd *WorkingDir) Close() error {
	return os.RemoveAll(wd.baseDir)
}

// Setenv sets an environment variable on the WorkingDir.
func (wd *WorkingDir) Setenv(envVar, val string) {
	if wd.env == nil {
		wd.env = map[string]string{}
	}
	wd.env[envVar] = val
}

// Unsetenv removes an environment variable from the WorkingDir.
func (wd *WorkingDir) Unsetenv(envVar string) {
	delete(wd.env, envVar)
}

func (wd *WorkingDir) SetReattachInfo(reattachInfo tfexec.ReattachInfo) {
	wd.reattachInfo = reattachInfo
}

func (wd *WorkingDir) UnsetReattachInfo() {
	wd.reattachInfo = nil
}

// GetHelper returns the Helper set on the WorkingDir.
func (wd *WorkingDir) GetHelper() *Helper {
	return wd.h
}

func (wd *WorkingDir) relativeConfigDir() (string, error) {
	relPath, err := filepath.Rel(wd.baseDir, wd.configDir)
	if err != nil {
		return "", fmt.Errorf("Error determining relative path of configuration directory: %w", err)
	}
	return relPath, nil
}

// SetConfig sets a new configuration for the working directory.
//
// This must be called at least once before any call to Init, Plan, Apply, or
// Destroy to establish the configuration. Any previously-set configuration is
// discarded and any saved plan is cleared.
func (wd *WorkingDir) SetConfig(cfg string) error {
	// Each call to SetConfig creates a new directory under our baseDir.
	// We create them within so that our final cleanup step will delete them
	// automatically without any additional tracking.
	configDir, err := ioutil.TempDir(wd.baseDir, "config")
	if err != nil {
		return err
	}
	configFilename := filepath.Join(configDir, "terraform_plugin_test.tf")
	err = ioutil.WriteFile(configFilename, []byte(cfg), 0700)
	if err != nil {
		return err
	}

	tf, err := tfexec.NewTerraform(wd.baseDir, wd.terraformExec)
	if err != nil {
		return err
	}

	var mismatch *tfexec.ErrVersionMismatch
	err = tf.SetDisablePluginTLS(true)
	if err != nil && !errors.As(err, &mismatch) {
		return err
	}
	err = tf.SetSkipProviderVerify(true)
	if err != nil && !errors.As(err, &mismatch) {
		return err
	}

	if p := os.Getenv("TF_ACC_LOG_PATH"); p != "" {
		tf.SetLogPath(p)
	}

	wd.configDir = configDir
	wd.tf = tf

	// Changing configuration invalidates any saved plan.
	err = wd.ClearPlan()
	if err != nil {
		return err
	}
	return nil
}

// ClearState deletes any Terraform state present in the working directory.
//
// Any remote objects tracked by the state are not destroyed first, so this
// will leave them dangling in the remote system.
func (wd *WorkingDir) ClearState() error {
	err := os.Remove(filepath.Join(wd.baseDir, "terraform.tfstate"))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// ClearPlan deletes any saved plan present in the working directory.
func (wd *WorkingDir) ClearPlan() error {
	err := os.Remove(wd.planFilename())
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// Init runs "terraform init" for the given working directory, forcing Terraform
// to use the current version of the plugin under test.
func (wd *WorkingDir) Init() error {
	if wd.configDir == "" {
		return fmt.Errorf("must call SetConfig before Init")
	}

	return wd.tf.Init(context.Background(), tfexec.Reattach(wd.reattachInfo), tfexec.Dir(wd.configDir))
}

func (wd *WorkingDir) planFilename() string {
	return filepath.Join(wd.baseDir, "tfplan")
}

// CreatePlan runs "terraform plan" to create a saved plan file, which if successful
// will then be used for the next call to Apply.
func (wd *WorkingDir) CreatePlan() error {
	_, err := wd.tf.Plan(context.Background(), tfexec.Reattach(wd.reattachInfo), tfexec.Refresh(false), tfexec.Out("tfplan"), tfexec.Dir(wd.configDir))
	return err
}

// CreateDestroyPlan runs "terraform plan -destroy" to create a saved plan
// file, which if successful will then be used for the next call to Apply.
func (wd *WorkingDir) CreateDestroyPlan() error {
	_, err := wd.tf.Plan(context.Background(), tfexec.Reattach(wd.reattachInfo), tfexec.Refresh(false), tfexec.Out("tfplan"), tfexec.Destroy(true), tfexec.Dir(wd.configDir))
	return err
}

// Apply runs "terraform apply". If CreatePlan has previously completed
// successfully and the saved plan has not been cleared in the meantime then
// this will apply the saved plan. Otherwise, it will implicitly create a new
// plan and apply it.
func (wd *WorkingDir) Apply() error {
	args := []tfexec.ApplyOption{tfexec.Reattach(wd.reattachInfo), tfexec.Refresh(false)}
	if wd.HasSavedPlan() {
		args = append(args, tfexec.DirOrPlan("tfplan"))
	} else {
		// we need to use a relative config dir here or we get an
		// error about Terraform not having any configuration. See
		// https://github.com/hashicorp/terraform-plugin-sdk/issues/495
		// for more info.
		configDir, err := wd.relativeConfigDir()
		if err != nil {
			return err
		}
		args = append(args, tfexec.DirOrPlan(configDir))
	}
	return wd.tf.Apply(context.Background(), args...)
}

// Destroy runs "terraform destroy". It does not consider or modify any saved
// plan, and is primarily for cleaning up at the end of a test run.
//
// If destroy fails then remote objects might still exist, and continue to
// exist after a particular test is concluded.
func (wd *WorkingDir) Destroy() error {
	return wd.tf.Destroy(context.Background(), tfexec.Reattach(wd.reattachInfo), tfexec.Refresh(false), tfexec.Dir(wd.configDir))
}

// HasSavedPlan returns true if there is a saved plan in the working directory. If
// so, a subsequent call to Apply will apply that saved plan.
func (wd *WorkingDir) HasSavedPlan() bool {
	_, err := os.Stat(wd.planFilename())
	return err == nil
}

// SavedPlan returns an object describing the current saved plan file, if any.
//
// If no plan is saved or if the plan file cannot be read, SavedPlan returns
// an error.
func (wd *WorkingDir) SavedPlan() (*tfjson.Plan, error) {
	if !wd.HasSavedPlan() {
		return nil, fmt.Errorf("there is no current saved plan")
	}

	return wd.tf.ShowPlanFile(context.Background(), wd.planFilename(), tfexec.Reattach(wd.reattachInfo))
}

// SavedPlanStdout returns a stdout capture of the current saved plan file, if any.
//
// If no plan is saved or if the plan file cannot be read, SavedPlanStdout returns
// an error.
func (wd *WorkingDir) SavedPlanStdout() (string, error) {
	if !wd.HasSavedPlan() {
		return "", fmt.Errorf("there is no current saved plan")
	}

	var ret bytes.Buffer

	wd.tf.SetStdout(&ret)
	defer wd.tf.SetStdout(ioutil.Discard)
	_, err := wd.tf.ShowPlanFile(context.Background(), wd.planFilename(), tfexec.Reattach(wd.reattachInfo))
	if err != nil {
		return "", err
	}

	return ret.String(), nil
}

// State returns an object describing the current state.
//

// If the state cannot be read, State returns an error.
func (wd *WorkingDir) State() (*tfjson.State, error) {
	return wd.tf.Show(context.Background(), tfexec.Reattach(wd.reattachInfo))
}

// Import runs terraform import
func (wd *WorkingDir) Import(resource, id string) error {
	return wd.tf.Import(context.Background(), resource, id, tfexec.Config(wd.configDir), tfexec.Reattach(wd.reattachInfo))
}

// Refresh runs terraform refresh
func (wd *WorkingDir) Refresh() error {
	return wd.tf.Refresh(context.Background(), tfexec.Reattach(wd.reattachInfo), tfexec.State(filepath.Join(wd.baseDir, "terraform.tfstate")), tfexec.Dir(wd.configDir))
}

// Schemas returns an object describing the provider schemas.
//
// If the schemas cannot be read, Schemas returns an error.
func (wd *WorkingDir) Schemas() (*tfjson.ProviderSchemas, error) {
	return wd.tf.ProvidersSchema(context.Background())
}
