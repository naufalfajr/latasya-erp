INSERT OR IGNORE INTO vehicles (code, capacity) VALUES ('LA003', 13);

UPDATE vehicles
SET capacity = 13
WHERE code IN ('LA001', 'LA002', 'LA003');

INSERT INTO vehicle_route_assignments (vehicle_id, route_id, starts_on)
SELECT v.id, r.id, date('now')
FROM vehicles v
JOIN routes r ON r.name = 'South'
WHERE v.code = 'LA003'
  AND NOT EXISTS (
      SELECT 1 FROM vehicle_route_assignments a
      WHERE a.route_id = r.id AND a.ends_on IS NULL
  )
  AND NOT EXISTS (
      SELECT 1 FROM vehicle_route_assignments a
      WHERE a.vehicle_id = v.id AND a.ends_on IS NULL
  );
