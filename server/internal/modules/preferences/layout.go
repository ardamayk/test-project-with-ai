package preferences

func clampPanelSizes(sizes []float64, collapsed map[string]bool) []float64 {
	const minLeft, maxLeft = 15.0, 45.0
	const minRight, maxRight = 18.0, 50.0
	const minMini, maxMini = 4.0, 12.0
	const minMain = 25.0

	left := sizes[0]
	right := sizes[2]

	if collapsed != nil && collapsed["left"] {
		if left < minMini {
			left = minMini
		}
		if left > maxMini {
			left = maxMini
		}
	} else {
		if left < minLeft {
			left = minLeft
		}
		if left > maxLeft {
			left = maxLeft
		}
	}

	if collapsed != nil && collapsed["right"] {
		if right < minMini {
			right = minMini
		}
		if right > maxMini {
			right = maxMini
		}
	} else {
		if right < minRight {
			right = minRight
		}
		if right > maxRight {
			right = maxRight
		}
	}

	main := 100 - left - right
	if main < minMain {
		deficit := minMain - main
		sideSum := left + right
		left -= (left / sideSum) * deficit
		right -= (right / sideSum) * deficit
		main = minMain
	}
	return []float64{left, main, right}
}

func normalizeLayout(layout LayoutPreferences) LayoutPreferences {
	if layout.SidebarPosition == "" {
		layout.SidebarPosition = "left"
	}
	if layout.Panels == nil {
		layout.Panels = defaultLayout().Panels
	}
	if layout.Collapsed == nil {
		layout.Collapsed = defaultLayout().Collapsed
	}
	if len(layout.Sizes) != 3 {
		layout.Sizes = defaultLayout().Sizes
	} else {
		layout.Sizes = clampPanelSizes(layout.Sizes, layout.Collapsed)
	}
	return layout
}
