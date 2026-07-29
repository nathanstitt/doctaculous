package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/nathanstitt/doctaculous/pkg/crop"
)

// parseCropSize parses a "WxH" pixel size. Both dimensions must be positive.
func parseCropSize(s string) (int, int, error) {
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == 'x' || r == 'X' })
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("--crop-size %q: want WxH, e.g. 720x720", s)
	}
	w, err := strconv.Atoi(parts[0])
	if err != nil || w <= 0 {
		return 0, 0, fmt.Errorf("--crop-size %q: width must be a positive integer", s)
	}
	h, err := strconv.Atoi(parts[1])
	if err != nil || h <= 0 {
		return 0, 0, fmt.Errorf("--crop-size %q: height must be a positive integer", s)
	}
	return w, h, nil
}

// parseCropStrategy maps a --crop value to a crop.Strategy.
func parseCropStrategy(s string) (crop.Strategy, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "center", "centre":
		return crop.StrategyCenter, nil
	case "north", "top":
		return crop.StrategyNorth, nil
	case "south", "bottom":
		return crop.StrategySouth, nil
	case "east", "right":
		return crop.StrategyEast, nil
	case "west", "left":
		return crop.StrategyWest, nil
	case "saliency", "smart":
		return crop.StrategySaliency, nil
	default:
		return 0, fmt.Errorf("--crop %q: want center, north, south, east, west or saliency", s)
	}
}

// cropOptions builds the crop options from the --crop/--crop-size pair. The two
// flags are required together: a strategy with no size has nothing to crop to,
// and a size with no strategy would silently pick one.
func cropOptions(mode, size string) (*crop.Options, error) {
	if mode == "" && size == "" {
		return nil, nil
	}
	if mode == "" || size == "" {
		return nil, fmt.Errorf("--crop and --crop-size must be given together")
	}
	strategy, err := parseCropStrategy(mode)
	if err != nil {
		return nil, err
	}
	w, h, err := parseCropSize(size)
	if err != nil {
		return nil, err
	}
	return &crop.Options{Strategy: strategy, Width: w, Height: h}, nil
}
