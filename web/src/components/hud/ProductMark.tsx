export function ProductMark({ size = 24 }: { size?: number }) {
  const cells = [4.5, 10, 15.5];
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" aria-hidden className="product-mark">
      <path d="M4 1H23V20L20 23H1V4Z" fill="none" stroke="var(--accent)" strokeWidth="1.5" />
      {cells.map((y) =>
        cells.map((x) =>
          x === 10 && y === 10 ? (
            <rect key={`${x}-${y}`} x={x} y={y} width="4" height="4" fill="var(--accent)" />
          ) : (
            <rect key={`${x}-${y}`} x={x + 0.75} y={y + 0.75} width="2.5" height="2.5" fill="none" stroke="currentColor" strokeWidth="1.5" />
          ),
        ),
      )}
    </svg>
  );
}
