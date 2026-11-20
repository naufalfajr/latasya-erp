package handler

import "testing"

func TestSeatPlan_Shape(t *testing.T) {
	t.Parallel()
	for _, n := range []int{1, 4, 5, 12, 13, 14, 16} {
		seats, drivers := 0, 0
		for _, row := range seatPlan(n) {
			if len(row) != 4 {
				t.Fatalf("seatPlan(%d): row %v is not 4 cells", n, row)
			}
			for _, cell := range row {
				switch cell {
				case "s":
					seats++
				case "d":
					drivers++
				}
			}
		}
		if seats != n || drivers != 1 {
			t.Errorf("seatPlan(%d) draws %d seats and %d drivers, want %d and 1", n, seats, drivers, n)
		}
	}
}
