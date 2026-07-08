package model

import (
	"database/sql"
	"fmt"
)

type Route struct {
	ID       int
	Name     string
	IsActive bool
}

type Vehicle struct {
	ID       int
	Code     string
	Capacity int
	IsActive bool
}

type RouteCapacity struct {
	RouteID     int
	RouteName   string
	VehicleCode string
	Capacity    int
	Used        int
}

func ListRoutes(db *sql.DB) ([]Route, error) {
	rows, err := db.Query(`SELECT id, name, is_active FROM routes WHERE is_active = 1 ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list routes: %w", err)
	}
	defer rows.Close()

	var routes []Route
	for rows.Next() {
		var r Route
		if err := rows.Scan(&r.ID, &r.Name, &r.IsActive); err != nil {
			return nil, fmt.Errorf("scan route: %w", err)
		}
		routes = append(routes, r)
	}
	return routes, nil
}

func ListVehicles(db *sql.DB) ([]Vehicle, error) {
	rows, err := db.Query(`SELECT id, code, capacity, is_active FROM vehicles WHERE is_active = 1 ORDER BY code`)
	if err != nil {
		return nil, fmt.Errorf("list vehicles: %w", err)
	}
	defer rows.Close()

	var vehicles []Vehicle
	for rows.Next() {
		var v Vehicle
		if err := rows.Scan(&v.ID, &v.Code, &v.Capacity, &v.IsActive); err != nil {
			return nil, fmt.Errorf("scan vehicle: %w", err)
		}
		vehicles = append(vehicles, v)
	}
	return vehicles, nil
}

func ListRouteCapacity(db *sql.DB) ([]RouteCapacity, error) {
	rows, err := db.Query(`SELECT r.id, r.name, COALESCE(v.code, ''), COALESCE(v.capacity, 0), COUNT(c.id)
		FROM routes r
		LEFT JOIN vehicle_route_assignments vra ON vra.route_id = r.id AND vra.ends_on IS NULL
		LEFT JOIN vehicles v ON v.id = vra.vehicle_id AND v.is_active = 1
		LEFT JOIN contacts c ON c.route_id = r.id AND c.is_active = 1
		WHERE r.is_active = 1
		GROUP BY r.id, r.name, v.code, v.capacity
		ORDER BY r.name`)
	if err != nil {
		return nil, fmt.Errorf("list route capacity: %w", err)
	}
	defer rows.Close()

	var capacities []RouteCapacity
	for rows.Next() {
		var c RouteCapacity
		if err := rows.Scan(&c.RouteID, &c.RouteName, &c.VehicleCode, &c.Capacity, &c.Used); err != nil {
			return nil, fmt.Errorf("scan route capacity: %w", err)
		}
		capacities = append(capacities, c)
	}
	return capacities, nil
}
