package crop

import "image"

// anchorWindow places a win-sized window inside bounds per strategy. The axis
// a gravity does not constrain is centered.
func anchorWindow(bounds image.Rectangle, win image.Point, s Strategy) image.Rectangle {
	slackX := bounds.Dx() - win.X
	slackY := bounds.Dy() - win.Y

	offX, offY := slackX/2, slackY/2
	switch s {
	case StrategyNorth:
		offY = 0
	case StrategySouth:
		offY = slackY
	case StrategyWest:
		offX = 0
	case StrategyEast:
		offX = slackX
	}

	origin := bounds.Min.Add(image.Pt(offX, offY))
	return image.Rectangle{Min: origin, Max: origin.Add(win)}
}
