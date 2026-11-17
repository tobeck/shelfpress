package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// DefaultFileName is the config file looked for in the working directory.
const DefaultFileName = ".shelfpress.yml"

// envPrefixes are the only environment variable namespaces the core reads.
// Adding a CI platform's prefix here would violate CLAUDE.md rule 1.
const (
	envPrefixShelfpress = "SHELFPRESS"
	envPrefixBookStack  = "BOOKSTACK"
)

// bookStackEnv maps a BookStack config key to its environment variable. These
// are bound explicitly because they do not share the SHELFPRESS_ prefix.
var bookStackEnv = map[string]string{
	"bookstack.url":          envPrefixBookStack + "_URL",
	"bookstack.token_id":     envPrefixBookStack + "_TOKEN_ID",
	"bookstack.token_secret": envPrefixBookStack + "_TOKEN_SECRET",
}

// Options controls how Load resolves configuration.
type Options struct {
	// File is an explicit config file path. When empty, DefaultFileName is
	// looked for in Dir and its absence is not an error.
	File string

	// Dir is the directory to resolve the default config file and the repo
	// fallback against. Defaults to the working directory.
	Dir string

	// Flags, when non-nil, supplies the highest-precedence values. Only flags
	// the user actually set are applied, so unset flags do not mask the
	// environment or config file.
	Flags *pflag.FlagSet
}

// Load resolves configuration from, in increasing order of precedence:
// defaults, the config file, the environment, and explicitly set flags.
//
// The returned Config is not validated; call Validate.
func Load(opts Options) (Config, error) {
	dir := opts.Dir
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return Config{}, fmt.Errorf("determining working directory: %w", err)
		}
		dir = wd
	}

	v := viper.New()
	v.SetDefault("root", DefaultRoot)
	v.SetDefault("prune", DefaultPrune)
	v.SetDefault("format", DefaultFormat)
	v.SetDefault("dry_run", false)

	if err := readFile(v, opts.File, dir); err != nil {
		return Config{}, err
	}

	bindEnv(v)

	if opts.Flags != nil {
		if err := bindSetFlags(v, opts.Flags); err != nil {
			return Config{}, err
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("decoding configuration: %w", err)
	}

	applyFallbacks(&cfg, dir)
	return cfg, nil
}

// readFile loads the config file into v. An explicitly requested file that is
// missing is an error; the default file being absent is not.
func readFile(v *viper.Viper, file, dir string) error {
	explicit := file != ""
	if !explicit {
		file = filepath.Join(dir, DefaultFileName)
	}

	v.SetConfigFile(file)
	v.SetConfigType("yaml")

	switch err := v.ReadInConfig(); {
	case err == nil:
		return nil
	case explicit:
		return fmt.Errorf("reading config file %s: %w", file, err)
	case os.IsNotExist(err):
		return nil
	default:
		// viper wraps a missing file in its own type on some paths; treat a
		// non-existent default file as absent rather than failing the run.
		if _, statErr := os.Stat(file); os.IsNotExist(statErr) {
			return nil
		}
		return fmt.Errorf("reading config file %s: %w", file, err)
	}
}

// bindEnv wires the SHELFPRESS_* and BOOKSTACK_* variables. No other prefix is
// read; see CLAUDE.md rule 1.
func bindEnv(v *viper.Viper) {
	v.SetEnvPrefix(envPrefixShelfpress)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	for key, envVar := range bookStackEnv {
		// BindEnv with an explicit variable name bypasses the prefix.
		_ = v.BindEnv(key, envVar)
	}
}

// flagToKey maps a CLI flag name to its configuration key.
var flagToKey = map[string]string{
	"root":         "root",
	"shelf":        "shelf",
	"repo":         "repo",
	"prune":        "prune",
	"dry-run":      "dry_run",
	"format":       "format",
	"output-file":  "output_file",
	"url":          "bookstack.url",
	"token-id":     "bookstack.token_id",
	"token-secret": "bookstack.token_secret",
}

// bindSetFlags applies only those flags the user actually set, so an unset flag
// carrying a zero value cannot override the environment or config file.
func bindSetFlags(v *viper.Viper, flags *pflag.FlagSet) error {
	var err error
	flags.Visit(func(f *pflag.Flag) {
		if err != nil {
			return
		}
		key, ok := flagToKey[f.Name]
		if !ok {
			return
		}
		if bindErr := v.BindPFlag(key, f); bindErr != nil {
			err = fmt.Errorf("binding flag --%s: %w", f.Name, bindErr)
		}
	})
	return err
}

// applyFallbacks fills values that are derived rather than configured.
func applyFallbacks(cfg *Config, dir string) {
	if cfg.Repo == "" {
		cfg.Repo = inferRepo(dir)
	}
	if cfg.Shelf == "" {
		cfg.Shelf = shelfFromRepo(cfg.Repo)
	}
}

// shelfFromRepo derives a default shelf name from a repo identifier, using the
// final path segment so "owner/repo" becomes "repo".
func shelfFromRepo(repo string) string {
	if repo == "" {
		return ""
	}
	if i := strings.LastIndex(repo, "/"); i >= 0 && i < len(repo)-1 {
		return repo[i+1:]
	}
	return repo
}
