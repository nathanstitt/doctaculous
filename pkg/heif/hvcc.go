package heif

import (
	"fmt"

	"github.com/nathanstitt/omnidoc/pkg/internal/hevc"
)

// hvcC — the HEVCDecoderConfigurationRecord (ISO/IEC 14496-15 §8.3.3.1) —
// carries the parameter-set NALs and the length-prefix size used by hvc1
// item payloads.
type hevcConfig struct {
	ps            hevc.ParamSets
	nalLengthSize int // 1, 2, or 4
}

// parseHVCC parses a raw hvcC property payload.
func parseHVCC(raw []byte) (*hevcConfig, error) {
	c := newCursor(raw, "hvcC")
	if v := c.u8(); v != 1 && !c.failed {
		return nil, fmt.Errorf("%w: hvcC configuration version %d", ErrUnsupported, v)
	}
	c.take(11)       // profile byte + compat(4) + constraints(6); authoritative copies live in the SPS
	c.u8()           // general_level_idc
	c.u16()          // reserved + min_spatial_segmentation_idc
	c.u8()           // reserved + parallelismType
	c.u8()           // reserved + chroma_format_idc
	c.u8()           // reserved + bit_depth_luma_minus8
	c.u8()           // reserved + bit_depth_chroma_minus8
	c.u16()          // avgFrameRate
	packed := c.u8() // constantFrameRate/numTemporalLayers/temporalIdNested/lengthSizeMinusOne
	numArrays := int(c.u8())
	if err := c.err(); err != nil {
		return nil, err
	}
	cfg := &hevcConfig{nalLengthSize: int(packed&3) + 1}
	for range numArrays {
		arr := c.u8() // completeness(1) + reserved(1) + NAL_unit_type(6)
		nalType := arr & 0x3f
		numNALs := int(c.u16())
		if numNALs > 64 {
			return nil, fmt.Errorf("%w: hvcC array with %d NALs", ErrInvalid, numNALs)
		}
		for range numNALs {
			n := int(c.u16())
			nal := c.take(n)
			if c.failed {
				break
			}
			switch nalType {
			case 32: // VPS
				cfg.ps.VPS = append(cfg.ps.VPS, nal)
			case 33: // SPS
				cfg.ps.SPS = append(cfg.ps.SPS, nal)
			case 34: // PPS
				cfg.ps.PPS = append(cfg.ps.PPS, nal)
			default:
				// SEI and future arrays are ignorable.
			}
		}
	}
	if err := c.err(); err != nil {
		return nil, err
	}
	if len(cfg.ps.SPS) == 0 || len(cfg.ps.PPS) == 0 {
		return nil, fmt.Errorf("%w: hvcC missing parameter sets", ErrInvalid)
	}
	return cfg, nil
}
