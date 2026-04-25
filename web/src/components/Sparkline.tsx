// Sparkline — a deliberately tiny inline-SVG line chart. We don't want
// to drag in recharts/uplot/etc. for a feature this small; ~50 lines
// of SVG covers what the system tab needs (one series, no axes, no
// legend, optional fill).
//
// Y-axis policy: the caller picks the domain via `min`/`max` (defaults
// to data min..max). For percent series, pass `min={0} max={100}` so
// every chart shares the 0-100 visual scale. For byte series, pass
// `min={0}` and let the max float so we don't waste pixels on the
// empty top half of the chart.

interface SparklineProps {
  values: number[];
  width?: number;
  height?: number;
  min?: number;
  max?: number;
  /** Tailwind classes appended to the stroke <path>. */
  strokeClass?: string;
  /** Tailwind classes appended to the fill <path>. */
  fillClass?: string;
  /** When true, draws a translucent area under the line. */
  filled?: boolean;
  className?: string;
  ariaLabel?: string;
}

export default function Sparkline({
  values,
  width = 240,
  height = 56,
  min,
  max,
  strokeClass = "stroke-sonar-400",
  fillClass = "fill-sonar-500/15",
  filled = true,
  className,
  ariaLabel,
}: SparklineProps) {
  if (!values.length) {
    return (
      <div
        className={
          "grid place-items-center text-xs text-slate-600 " + (className ?? "")
        }
        style={{ width, height }}
      >
        no data
      </div>
    );
  }

  const xMin = 0;
  const xMax = Math.max(values.length - 1, 1);
  const dataMin = Math.min(...values);
  const dataMax = Math.max(...values);
  const yMin = min ?? dataMin;
  // Floor the visual range to a small positive span so a flat-zero
  // series doesn't divide-by-zero (and renders as a baseline).
  const yMax = Math.max(max ?? dataMax, yMin + 0.0001);

  const pad = 2; // keep the stroke from clipping at the edges

  const x = (i: number) =>
    pad + ((i - xMin) / (xMax - xMin)) * (width - pad * 2);
  const y = (v: number) =>
    height - pad - ((v - yMin) / (yMax - yMin)) * (height - pad * 2);

  const linePath = values
    .map((v, i) => `${i === 0 ? "M" : "L"} ${x(i).toFixed(2)} ${y(v).toFixed(2)}`)
    .join(" ");

  const areaPath = filled
    ? `${linePath} L ${x(xMax).toFixed(2)} ${(height - pad).toFixed(2)} L ${x(0).toFixed(2)} ${(height - pad).toFixed(2)} Z`
    : "";

  return (
    <svg
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      className={className}
      role="img"
      aria-label={ariaLabel}
    >
      {filled && <path d={areaPath} className={fillClass} stroke="none" />}
      <path d={linePath} className={strokeClass} strokeWidth="1.5" fill="none" />
    </svg>
  );
}
