package config

import (
	"fmt"
	"regexp"
)

var nameRe = regexp.MustCompile(`^[A-Za-z0-9_.\-]{1,128}$`)

func Validate(c *Config) error {
	if c.HTTP.Port < 1 || c.HTTP.Port > 65535 {
		return fmt.Errorf("http port %d is out of range", c.HTTP.Port)
	}
	for name, s := range c.Streams {
		if !nameRe.MatchString(name) {
			return fmt.Errorf("stream name %q is not a valid identifier", name)
		}
		if len(s.Inputs) == 0 && !s.NoMedia && !s.Ondemand {
			return fmt.Errorf("stream %q has no inputs", name)
		}
		for _, in := range s.Inputs {
			if in.URL == "" {
				return fmt.Errorf("stream %q has an input without a URL", name)
			}
		}
		if s.DVR != nil && s.DVR.Reference != "" {
			if _, ok := c.DVRs[s.DVR.Reference]; !ok {
				return fmt.Errorf("stream %q references unknown dvr %q", name, s.DVR.Reference)
			}
		}
	}
	for name, d := range c.DVRs {
		if !nameRe.MatchString(name) {
			return fmt.Errorf("dvr name %q is not a valid identifier", name)
		}
		if d.Root == "" {
			return fmt.Errorf("dvr %q has no root", name)
		}
	}
	for name, b := range c.Backends {
		if !nameRe.MatchString(name) {
			return fmt.Errorf("auth_backend name %q is not a valid identifier", name)
		}
		if len(b.Backends) == 0 && len(b.AllowTokens) == 0 && len(b.AllowIPs) == 0 &&
			len(b.AllowCountries) == 0 && len(b.AllowUAs) == 0 {
			return fmt.Errorf("auth_backend %q has no rules", name)
		}
	}
	return nil
}
