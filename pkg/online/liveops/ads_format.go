package liveops

import (
	"bytes"
	"fmt"
	"go.yaml.in/yaml/v3"
	"time"
)

// AdsConfigVersion is the version of ads.yaml.
const AdsConfigVersion = 1

// AdsConfig declares ad placements and interstitial policy per platform.
type AdsConfig struct {
	Version   int                    `yaml:"version"`
	Platforms map[string]AdsPlatform `yaml:"platforms"`
}

// AdsPlatform contains host ad unit IDs and interstitial limits.
type AdsPlatform struct {
	Rewarded             map[string]string `yaml:"rewarded,omitempty"`
	Interstitial         map[string]string `yaml:"interstitial,omitempty"`
	Banner               map[string]string `yaml:"banner,omitempty"`
	InterstitialInterval time.Duration     `yaml:"interstitialInterval,omitempty"`
	InterstitialCap      int               `yaml:"interstitialCap,omitempty"`
}

// ParseAdsConfig strictly parses ads.yaml.
func ParseAdsConfig(data []byte) (*AdsConfig, error) {
	var c AdsConfig
	d := yaml.NewDecoder(bytes.NewReader(data))
	d.KnownFields(true)
	if err := d.Decode(&c); err != nil {
		return nil, fmt.Errorf("liveops: decode ads.yaml: %w", err)
	}
	var extra any
	if err := d.Decode(&extra); err == nil {
		return nil, fmt.Errorf("liveops: ads.yaml: trailing document")
	}
	if err := ValidateAdsConfig(&c); err != nil {
		return nil, err
	}
	return &c, nil
}

// ValidateAdsConfig validates version, platform names, unit IDs and limits.
func ValidateAdsConfig(c *AdsConfig) error {
	if c == nil || c.Version != AdsConfigVersion {
		return fmt.Errorf("liveops: ads.yaml: version must be %d", AdsConfigVersion)
	}
	for platform, p := range c.Platforms {
		if platform == "" {
			return fmt.Errorf("liveops: ads.yaml: platform name is required")
		}
		if p.InterstitialCap < 0 {
			return fmt.Errorf("liveops: ads.yaml: %s interstitialCap must be non-negative", platform)
		}
		if p.InterstitialInterval < 0 {
			return fmt.Errorf("liveops: ads.yaml: %s interstitialInterval must be non-negative", platform)
		}
		for _, m := range []map[string]string{p.Rewarded, p.Interstitial, p.Banner} {
			for placement, id := range m {
				if placement == "" || id == "" {
					return fmt.Errorf("liveops: ads.yaml: %s placement and unit ID are required", platform)
				}
			}
		}
	}
	return nil
}

// EncodeAdsConfig writes deterministic two-space YAML.
func EncodeAdsConfig(c *AdsConfig) ([]byte, error) {
	if err := ValidateAdsConfig(c); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	e := yaml.NewEncoder(&b)
	e.SetIndent(2)
	if err := e.Encode(c); err != nil {
		return nil, err
	}
	_ = e.Close()
	return b.Bytes(), nil
}
