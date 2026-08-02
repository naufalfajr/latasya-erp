package contact

import (
	"context"
	"fmt"
)

type Route struct {
	ID       int
	Name     string
	IsActive bool
}

type RouteCapacity struct {
	RouteID     int
	RouteName   string
	VehicleCode string
	Capacity    int
	Used        int
}

func (m *Module) ListRoutes(ctx context.Context) ([]Route, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT id, name, is_active FROM routes WHERE is_active = 1 ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list routes: %w", err)
	}
	defer rows.Close()
	routes := []Route{}
	for rows.Next() {
		var route Route
		if err := rows.Scan(&route.ID, &route.Name, &route.IsActive); err != nil {
			return nil, fmt.Errorf("scan route: %w", err)
		}
		routes = append(routes, route)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate routes: %w", err)
	}
	return routes, nil
}

func (m *Module) ListRouteCapacity(ctx context.Context) ([]RouteCapacity, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT r.id, r.name, COALESCE(v.code, ''), COALESCE(v.capacity, 0), COUNT(c.id)
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
	capacities := []RouteCapacity{}
	for rows.Next() {
		var capacity RouteCapacity
		if err := rows.Scan(&capacity.RouteID, &capacity.RouteName, &capacity.VehicleCode, &capacity.Capacity, &capacity.Used); err != nil {
			return nil, fmt.Errorf("scan route capacity: %w", err)
		}
		capacities = append(capacities, capacity)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate route capacity: %w", err)
	}
	return capacities, nil
}
