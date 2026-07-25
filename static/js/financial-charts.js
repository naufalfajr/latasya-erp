(function (global) {
    "use strict";

    const NS = "http://www.w3.org/2000/svg";
    const money = new Intl.NumberFormat("id-ID", {style: "currency", currency: "IDR", maximumFractionDigits: 0});
    const colors = {revenue: "#16a34a", expense: "#dc2626", line: "#2563eb", positive: "#16a34a", negative: "#dc2626", grid: "#94a3b8"};

    function svgNode(name, attrs) {
        const node = document.createElementNS(NS, name);
        Object.entries(attrs || {}).forEach(([key, value]) => node.setAttribute(key, value));
        return node;
    }

    function dimensions(el, height, count, showLabels) {
        const width = Math.max(640, count * 64, el.clientWidth || 640);
        return {width, height, left: 72, right: 20, top: 20, bottom: showLabels ? 46 : 20};
    }

    function bounds(values) {
        let min = Math.min(0, ...values);
        let max = Math.max(0, ...values);
        if (min === max) max = min + 1;
        const pad = (max - min) * 0.1;
        return {min: min - pad, max: max + pad};
    }

    function plot(el, height, data, valueSets, draw, clickURL, reportLabel, showLabels = true) {
        if (!el) return;
        el.textContent = "";
        el.classList.add("overflow-x-auto");
        const d = dimensions(el, height, data.length, showLabels);
        const svg = svgNode("svg", {viewBox: `0 0 ${d.width} ${height}`, width: d.width, height});
        svg.style.display = "block";
        const innerW = d.width - d.left - d.right;
        const innerH = height - d.top - d.bottom;
        const all = valueSets.flat();
        const range = bounds(all);
        const y = value => d.top + (range.max - value) / (range.max - range.min) * innerH;
        const x = index => d.left + (index + 0.5) * innerW / data.length;
        const zero = y(0);
        svg.appendChild(svgNode("line", {x1: d.left, y1: zero, x2: d.width - d.right, y2: zero, stroke: colors.grid, "stroke-width": 1.5}));
        if (showLabels) {
            data.forEach((row, index) => {
                const label = svgNode("text", {x: x(index), y: height - 16, "text-anchor": "middle", "font-size": 11, fill: "currentColor"});
                label.textContent = row.month + (row.is_partial ? " MTD" : "");
                svg.appendChild(label);
            });
        }
        draw(svg, {x, y, zero, innerW, innerH, d});
        const tip = document.createElement("div");
        tip.className = "fixed z-50 hidden rounded bg-neutral text-neutral-content px-3 py-2 text-xs shadow-lg pointer-events-none";
        document.body.appendChild(tip);
        data.forEach((row, index) => {
            const hit = svgNode("rect", {
                x: d.left + index * innerW / data.length, y: d.top,
                width: innerW / data.length, height: innerH, fill: "transparent", cursor: "pointer",
                role: "link", tabindex: "0",
                "aria-label": `Open ${reportLabel} for ${row.month}${row.is_partial ? " MTD" : ""}`
            });
            const show = event => {
                tip.innerHTML = row.tooltip;
                tip.classList.remove("hidden");
                const point = event.touches ? event.touches[0] : event;
                tip.style.left = `${Math.min(global.innerWidth - 220, point.clientX + 12)}px`;
                tip.style.top = `${Math.max(8, point.clientY - 60)}px`;
            };
            hit.addEventListener("mousemove", show);
            hit.addEventListener("touchstart", show, {passive: true});
            hit.addEventListener("mouseleave", () => tip.classList.add("hidden"));
            hit.addEventListener("focus", () => {
                const rect = hit.getBoundingClientRect();
                show({clientX: rect.left + rect.width / 2, clientY: rect.top});
            });
            hit.addEventListener("blur", () => tip.classList.add("hidden"));
            const go = () => { global.location.href = clickURL(row); };
            hit.addEventListener("click", go);
            hit.addEventListener("keydown", event => { if (event.key === "Enter" || event.key === " ") go(); });
            svg.appendChild(hit);
        });
        el.appendChild(svg);
    }

    function reportURL(basePath, report, row) {
        return `${basePath}/reports/${report}?from=${row.start_date}&to=${row.end_date}`;
    }

    function legend(text) {
        const item = document.createElement("p");
        item.className = "mb-2 text-xs opacity-80";
        item.textContent = text;
        return item;
    }

    function profitability(el, trends, basePath) {
        const rows = trends.map(row => ({
            ...row,
            tooltip: `<strong>${row.month}${row.is_partial ? " MTD" : ""}</strong><br>Revenue: ${money.format(row.revenue)}<br>Expenses: ${money.format(row.expenses)}<br>Net income: ${money.format(row.net_income)}`
        }));
        plot(el, 320, rows, [rows.map(r => r.revenue), rows.map(r => r.expenses), rows.map(r => r.net_income)], (svg, p) => {
            const defs = svgNode("defs");
            const pattern = svgNode("pattern", {id: "expense-hatch", width: 8, height: 8, patternUnits: "userSpaceOnUse"});
            pattern.appendChild(svgNode("rect", {width: 8, height: 8, fill: colors.expense}));
            pattern.appendChild(svgNode("path", {d: "M-2,2 L2,-2 M0,8 L8,0 M6,10 L10,6", stroke: "#ffffff", "stroke-width": 2}));
            defs.appendChild(pattern);
            svg.appendChild(defs);
            const step = p.innerW / rows.length;
            const barW = Math.max(3, Math.min(18, step * 0.28));
            rows.forEach((row, i) => {
                [[row.revenue, -barW, colors.revenue], [row.expenses, 0, "url(#expense-hatch)"]].forEach(([value, offset, fill]) => {
                    svg.appendChild(svgNode("rect", {x: p.x(i) + offset, y: Math.min(p.y(value), p.zero), width: barW, height: Math.max(1, Math.abs(p.y(value) - p.zero)), fill}));
                });
            });
            const points = rows.map((row, i) => `${p.x(i)},${p.y(row.net_income)}`).join(" ");
            svg.appendChild(svgNode("polyline", {points, fill: "none", stroke: colors.line, "stroke-width": 3}));
            rows.forEach((row, i) => svg.appendChild(svgNode("circle", {cx: p.x(i), cy: p.y(row.net_income), r: 4, fill: colors.line})));
        }, row => reportURL(basePath, "profit-loss", row), "Profit & Loss report");
        el.prepend(legend("Revenue: solid bars · Expenses: diagonally striped bars · Net income: line with circle points"));
    }

    function cashPosition(el, trends, basePath) {
        el.textContent = "";
        el.classList.add("overflow-x-auto");
        el.append(legend("Closing cash: line with circle points · Net movement: bars above or below the zero baseline"));
        const plots = document.createElement("div");
        plots.style.minWidth = `${Math.max(640, trends.length * 64)}px`;
        const top = document.createElement("div");
        const lower = document.createElement("div");
        plots.append(top, lower);
        el.append(plots);
        const rows = trends.map(row => ({
            ...row,
            tooltip: `<strong>${row.month}${row.is_partial ? " MTD" : ""}</strong><br>Closing cash: ${money.format(row.closing_cash)}<br>Net movement: ${money.format(row.net_cash_movement)}`
        }));
        plot(top, 220, rows, [rows.map(r => r.closing_cash)], (svg, p) => {
            const points = rows.map((row, i) => `${p.x(i)},${p.y(row.closing_cash)}`).join(" ");
            svg.appendChild(svgNode("polyline", {points, fill: "none", stroke: colors.line, "stroke-width": 3}));
            rows.forEach((row, i) => svg.appendChild(svgNode("circle", {cx: p.x(i), cy: p.y(row.closing_cash), r: 4, fill: row.closing_cash < 0 ? colors.negative : colors.line})));
        }, row => reportURL(basePath, "cash-flow", row), "Cash Flow report", false);
        plot(lower, 200, rows, [rows.map(r => r.net_cash_movement)], (svg, p) => {
            const barW = Math.max(4, Math.min(28, p.innerW / rows.length * 0.55));
            rows.forEach((row, i) => svg.appendChild(svgNode("rect", {
                x: p.x(i) - barW / 2, y: Math.min(p.y(row.net_cash_movement), p.zero), width: barW,
                height: Math.max(1, Math.abs(p.y(row.net_cash_movement) - p.zero)),
                fill: row.net_cash_movement < 0 ? colors.negative : colors.positive
            })));
        }, row => reportURL(basePath, "cash-flow", row), "Cash Flow report");
    }

    global.FinancialCharts = {profitability, cashPosition};
})(window);
