(function (global) {
    "use strict";

    const NS = "http://www.w3.org/2000/svg";
    const money = new Intl.NumberFormat("id-ID", {style: "currency", currency: "IDR", maximumFractionDigits: 0});
    const compactMoney = new Intl.NumberFormat("id-ID", {style: "currency", currency: "IDR", notation: "compact", maximumFractionDigits: 1, minimumFractionDigits: 0});
    // Theme tokens, not hex: the charts follow light/dark without a redraw.
    // One accent (DESIGN.md "Mono Chart"): revenue in brand navy, expenses in
    // textured grey, the net/closing line in ink; losses read by sign, not red.
    const surface = "var(--paper)";
    const colors = {
        revenue: "var(--brand)",
        expense: "color-mix(in srgb, var(--mute) 55%, var(--paper))",
        expenseTexture: "var(--mute)",
        accent: "var(--ink)",
    };

    // fill/stroke holding var() go through style: SVG presentation
    // attributes do not resolve custom properties in every browser.
    function svgNode(name, attrs) {
        const node = document.createElementNS(NS, name);
        Object.entries(attrs || {}).forEach(([key, value]) => {
            if ((key === "fill" || key === "stroke") && /var\(/.test(value)) node.style[key] = value;
            else node.setAttribute(key, value);
        });
        return node;
    }

    function el(tag, attrs, children) {
        const node = document.createElement(tag);
        Object.entries(attrs || {}).forEach(([key, value]) => {
            if (key === "class") node.className = value;
            else node.setAttribute(key, value);
        });
        (children || []).forEach(c => node.appendChild(c));
        return node;
    }

    function niceTicks(min, max, count) {
        if (min === max) { min -= 1; max += 1; }
        const rawStep = (max - min) / count;
        const mag = Math.pow(10, Math.floor(Math.log10(Math.abs(rawStep))));
        const norm = rawStep / mag;
        const step = (norm >= 5 ? 10 : norm >= 2 ? 5 : norm >= 1 ? 2 : 1) * mag;
        const ticks = [];
        for (let v = Math.ceil(min / step) * step; v <= max + step * 1e-6; v += step) ticks.push(Math.round(v));
        return ticks;
    }

    // Phones fit the plot to the card so the y-axis scale stays in view;
    // wider screens keep a 560px floor and scroll if a card is narrower.
    // An unlaid-out container (clientWidth 0) counts as 560 wide, never 0.
    function containerWidth(container) {
        return container.clientWidth || 560;
    }

    function isNarrow(container) {
        return containerWidth(container) < 640;
    }

    function dimensions(container, count, showXLabels) {
        const width = isNarrow(container) ? containerWidth(container) : Math.max(560, count * 84, containerWidth(container));
        return {width, left: 60, right: 16, top: 12, bottom: showXLabels ? 40 : 8};
    }

    // Draws the shared scaffold — surface, recessive y-gridlines with compact
    // labels, zero baseline, x-axis period labels — and returns the scale
    // helpers marks are drawn with.
    function frame(container, height, rows, scaleValues, showXLabels) {
        container.textContent = "";
        container.classList.add("overflow-x-auto");
        const d = dimensions(container, rows.length, showXLabels);
        const svg = svgNode("svg", {viewBox: `0 0 ${d.width} ${height}`, width: d.width, height});
        svg.style.display = "block";
        const innerW = d.width - d.left - d.right;
        const innerH = height - d.top - d.bottom;

        let min = Math.min(0, ...scaleValues), max = Math.max(0, ...scaleValues);
        if (min === max) max = min + 1;
        const pad = (max - min) * 0.12;
        const range = {min: min - pad, max: max + pad};
        const y = value => d.top + (range.max - value) / (range.max - range.min) * innerH;
        const x = index => d.left + (index + 0.5) * innerW / rows.length;
        const zero = y(0);
        const slotWidth = innerW / rows.length;

        niceTicks(range.min, range.max, 4).forEach(tick => {
            const ty = y(tick);
            svg.appendChild(svgNode("line", {
                x1: d.left, y1: ty, x2: d.width - d.right, y2: ty,
                stroke: "currentColor", "stroke-opacity": 0.1, "stroke-width": 1,
            }));
            const label = svgNode("text", {
                x: d.left - 8, y: ty, "text-anchor": "end", "dominant-baseline": "middle",
                "font-size": 10, fill: "currentColor", opacity: 0.55,
            });
            label.textContent = compactMoney.format(tick + 0); // + 0 turns -0 into 0, no "-Rp 0"
            svg.appendChild(label);
        });
        svg.appendChild(svgNode("line", {
            x1: d.left, y1: zero, x2: d.width - d.right, y2: zero,
            stroke: "currentColor", "stroke-opacity": 0.35, "stroke-width": 1,
        }));
        if (showXLabels) {
            rows.forEach((row, i) => {
                const label = svgNode("text", {
                    x: x(i), y: height - 14, "text-anchor": "middle", "font-size": 11,
                    fill: "currentColor", opacity: row.is_partial ? 1 : 0.7,
                    "font-weight": row.is_partial ? 600 : 400,
                });
                // "Jun 2026" -> "Jun" on phones; the tooltip and table keep the year.
                label.textContent = isNarrow(container) ? row.label.split(" ")[0] : row.label;
                svg.appendChild(label);
            });
        }
        container.appendChild(svg);
        // Narrow screens scroll the plot; open on the latest period.
        container.scrollLeft = container.scrollWidth;
        return {svg, d, x, y, zero, innerW, innerH, slotWidth};
    }

    // One tooltip element per top-level chart container, reused across a
    // chart's sub-plots and across redraws (a stale one is removed first —
    // draw() can re-run on container resize).
    function sharedTooltip(container) {
        if (container._chartTip) container._chartTip.remove();
        const tip = el("div", {class: "chart-tip hidden"});
        document.body.appendChild(tip);
        container._chartTip = tip;
        return tip;
    }

    // One hover/focus/click target per period: a vertical guide line that
    // snaps to the nearest period, plus a tooltip built from DOM nodes
    // (never innerHTML — labels come from server data).
    function attachHover(svg, f, rows, buildTooltipRows, clickURL, reportLabel, tip) {
        const guide = svgNode("line", {
            y1: f.d.top, y2: f.d.top + f.innerH, stroke: "currentColor", "stroke-opacity": 0, "stroke-width": 1,
        });
        svg.appendChild(guide);

        rows.forEach((row, index) => {
            const hit = svgNode("rect", {
                x: f.d.left + index * f.slotWidth, y: f.d.top, width: f.slotWidth, height: f.innerH,
                fill: "transparent", cursor: "pointer", role: "link", tabindex: "0",
                "aria-label": `Open ${reportLabel} for ${row.label}${row.is_partial ? ", in progress" : ""}`,
            });
            const show = event => {
                guide.setAttribute("x1", f.x(index));
                guide.setAttribute("x2", f.x(index));
                guide.setAttribute("stroke-opacity", 0.25);
                tip.textContent = "";
                const title = el("div", {class: "mb-1 font-medium text-ink"});
                title.textContent = row.label + (row.is_partial ? " (in progress)" : "");
                tip.appendChild(title);
                buildTooltipRows(row).forEach(r => tip.appendChild(r));
                tip.classList.remove("hidden");
                const point = event.touches ? event.touches[0] : event;
                tip.style.left = `${Math.min(global.innerWidth - 200, Math.max(8, point.clientX + 12))}px`;
                tip.style.top = `${Math.max(8, point.clientY - 60)}px`;
            };
            const hide = () => { tip.classList.add("hidden"); guide.setAttribute("stroke-opacity", 0); };
            hit.addEventListener("mousemove", show);
            hit.addEventListener("touchstart", show, {passive: true});
            hit.addEventListener("mouseleave", hide);
            hit.addEventListener("focus", () => {
                const rect = hit.getBoundingClientRect();
                show({clientX: rect.left + rect.width / 2, clientY: rect.top});
            });
            hit.addEventListener("blur", hide);
            const go = () => { global.location.href = clickURL(row); };
            hit.addEventListener("click", go);
            hit.addEventListener("keydown", event => { if (event.key === "Enter" || event.key === " ") go(); });
            svg.appendChild(hit);
        });
    }

    // One tooltip row: a swatch/line-key in the series color, the label in
    // secondary ink, and the value leading in bold — text never wears the
    // data color.
    function tooltipRow(kind, color, label, value) {
        const swatch = el("span", {class: kind === "line" ? "inline-block w-3 h-0.5 align-middle" : "inline-block w-2.5 h-2.5 align-middle rounded-sm"});
        swatch.style.background = color;
        const labelSpan = el("span", {class: "flex items-center gap-1.5 text-ink-2"}, [swatch]);
        labelSpan.appendChild(document.createTextNode(label));
        const valueEl = el("strong", {class: "font-mono font-medium tabular-nums text-ink"});
        valueEl.textContent = money.format(value);
        return el("div", {class: "flex items-center justify-between gap-3 py-0.5"}, [labelSpan, valueEl]);
    }

    function legend(items) {
        const row = el("div", {class: "mb-3 flex flex-wrap items-center gap-x-4 gap-y-1.5 text-[12px] text-ink-2"});
        items.forEach(item => {
            const swatch = el("span", {class: item.kind === "line" ? "inline-block w-3.5 h-0.5 rounded-full" : "inline-block w-2.5 h-2.5 rounded-sm"});
            swatch.style.background = item.color;
            const wrap = el("span", {class: "inline-flex items-center gap-1.5"}, [swatch]);
            wrap.appendChild(document.createTextNode(item.label));
            row.appendChild(wrap);
        });
        return row;
    }

    // Bar path with a 4px rounded data-end and a square baseline end,
    // direction-aware so it works for bars growing up or down from zero.
    function barPath(x, w, valueY, baselineY, radius) {
        const top = Math.min(valueY, baselineY);
        const bottom = Math.max(valueY, baselineY);
        const r = Math.min(radius, w / 2, bottom - top);
        if (r <= 0.5) return `M${x},${top} L${x + w},${top} L${x + w},${bottom} L${x},${bottom} Z`;
        if (valueY < baselineY) {
            return `M${x},${bottom} L${x},${top + r} Q${x},${top} ${x + r},${top} L${x + w - r},${top} Q${x + w},${top} ${x + w},${top + r} L${x + w},${bottom} Z`;
        }
        return `M${x},${top} L${x + w},${top} L${x + w},${bottom - r} Q${x + w},${bottom} ${x + w - r},${bottom} L${x + r},${bottom} Q${x},${bottom} ${x},${bottom - r} Z`;
    }

    function bar(svg, x, w, y, zero, value, fill, radius) {
        svg.appendChild(svgNode("path", {d: barPath(x, w, y(value), zero, radius), fill}));
    }

    function marker(svg, cx, y, value, color) {
        svg.appendChild(svgNode("circle", {cx, cy: y(value), r: 4, fill: color, stroke: surface, "stroke-width": 2}));
    }

    function reportURL(basePath, report, row) {
        return `${basePath}/reports/${report}?from=${row.start_date}&to=${row.end_date}`;
    }

    function profitability(container, trends, basePath) {
        if (!container) return;
        const scaleValues = trends.flatMap(r => [r.revenue, r.expenses, r.net_income]);

        // Redrawn on every container size change (not just once at load): a
        // cross-document view-transition or web-font swap can settle the
        // layout *after* this script first runs, and the chart is sized from
        // container.clientWidth.
        function draw() {
            container.textContent = "";
            const tip = sharedTooltip(container);
            const wrap = el("div", {});
            wrap.appendChild(legend([
                {kind: "swatch", color: colors.revenue, label: "Revenue"},
                {kind: "swatch", color: colors.expense, label: "Expenses (textured)"},
                {kind: "line", color: colors.accent, label: "Net income"},
            ]));
            const plotEl = el("div", {});
            wrap.appendChild(plotEl);
            container.appendChild(wrap);

            const f = frame(plotEl, 300, trends, scaleValues, true);
            const defs = svgNode("defs");
            const pattern = svgNode("pattern", {id: "expense-texture", width: 6, height: 6, patternUnits: "userSpaceOnUse", patternTransform: "rotate(45)"});
            pattern.appendChild(svgNode("rect", {width: 6, height: 6, fill: colors.expense}));
            pattern.appendChild(svgNode("line", {x1: 0, y1: 0, x2: 0, y2: 6, stroke: colors.expenseTexture, "stroke-width": 1.5}));
            defs.appendChild(pattern);
            f.svg.appendChild(defs);

            const barW = Math.max(6, Math.min(22, f.slotWidth * 0.3));
            const gap = 2;
            trends.forEach((row, i) => {
                bar(f.svg, f.x(i) - barW - gap / 2, barW, f.y, f.zero, row.revenue, colors.revenue, 4);
                bar(f.svg, f.x(i) + gap / 2, barW, f.y, f.zero, row.expenses, "url(#expense-texture)", 4);
            });

            const points = trends.map((row, i) => `${f.x(i)},${f.y(row.net_income)}`).join(" ");
            f.svg.appendChild(svgNode("polyline", {points, fill: "none", stroke: colors.accent, "stroke-width": 2, "stroke-linejoin": "round", "stroke-linecap": "round"}));
            trends.forEach((row, i) => marker(f.svg, f.x(i), f.y, row.net_income, colors.accent));

            attachHover(f.svg, f, trends, row => [
                tooltipRow("swatch", colors.revenue, "Revenue", row.revenue),
                tooltipRow("swatch", colors.expense, "Expenses", row.expenses),
                tooltipRow("line", colors.accent, "Net income", row.net_income),
            ], row => reportURL(basePath, "profit-loss", row), "Profit & Loss report", tip);
        }
        draw();
        new ResizeObserver(draw).observe(container);
    }

    function cashPosition(container, trends, basePath) {
        if (!container) return;
        const closingValues = trends.map(r => r.closing_cash);
        const movementValues = trends.map(r => r.net_cash_movement);

        function draw() {
            container.textContent = "";
            const tip = sharedTooltip(container);
            const wrap = el("div", {});
            wrap.appendChild(legend([
                {kind: "line", color: colors.accent, label: "Closing cash"},
                {kind: "swatch", color: colors.revenue, label: "Cash inflow"},
                {kind: "swatch", color: colors.expense, label: "Cash outflow"},
            ]));
            const topEl = el("div", {});
            const lowerEl = el("div", {});
            wrap.appendChild(topEl);
            wrap.appendChild(lowerEl);
            container.appendChild(wrap);

            const top = frame(topEl, 180, trends, closingValues, false);
            const points = trends.map((row, i) => `${top.x(i)},${top.y(row.closing_cash)}`).join(" ");
            top.svg.appendChild(svgNode("polyline", {points, fill: "none", stroke: colors.accent, "stroke-width": 2, "stroke-linejoin": "round", "stroke-linecap": "round"}));
            trends.forEach((row, i) => marker(top.svg, top.x(i), top.y, row.closing_cash, colors.accent));
            attachHover(top.svg, top, trends, row => [
                tooltipRow("line", colors.accent, "Closing cash", row.closing_cash),
            ], row => reportURL(basePath, "cash-flow", row), "Cash Flow report", tip);

            const lower = frame(lowerEl, 200, trends, movementValues, true);
            const barW = Math.max(8, Math.min(28, lower.slotWidth * 0.55));
            trends.forEach((row, i) => {
                const color = row.net_cash_movement < 0 ? colors.expense : colors.revenue;
                bar(lower.svg, lower.x(i) - barW / 2, barW, lower.y, lower.zero, row.net_cash_movement, color, 4);
            });
            attachHover(lower.svg, lower, trends, row => [
                tooltipRow("swatch", row.net_cash_movement < 0 ? colors.expense : colors.revenue, "Net movement", row.net_cash_movement),
            ], row => reportURL(basePath, "cash-flow", row), "Cash Flow report", tip);
        }
        draw();
        new ResizeObserver(draw).observe(container);
    }

    global.FinancialCharts = {profitability, cashPosition};
})(window);
