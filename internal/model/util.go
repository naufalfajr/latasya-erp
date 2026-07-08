package model

func nullInt(v int) any {
	if v == 0 {
		return nil
	}
	return v
}
