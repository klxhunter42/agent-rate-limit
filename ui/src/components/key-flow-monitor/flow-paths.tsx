import { useEffect, useRef, useState, useCallback } from 'react';
import type { KeyStatusEntry, ModelStatus } from '@/lib/api';

const PATH_COLORS = [
  '#1e6091', '#2d8a6e', '#d4a012', '#c92a2d', '#c45a1a',
  '#6b9c4d', '#3d5a73', '#cc7614', '#3a7371', '#7c5fc4',
];

interface PathDef {
  d: string;
  keyIdx: number | null;
  modelIdx: number;
}

interface FlowPathsProps {
  keys: KeyStatusEntry[];
  models: ModelStatus[];
  hoveredKey: number | null;
  hoveredModel: number | null;
}

export function FlowPaths({ keys, models, hoveredKey, hoveredModel }: FlowPathsProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const [paths, setPaths] = useState<PathDef[]>([]);

  const calculatePaths = useCallback(() => {
    if (!containerRef.current) return;
    const container = containerRef.current.getBoundingClientRect();
    const gatewayEl = containerRef.current.querySelector<HTMLElement>('[data-gateway]');
    const keyNodes = containerRef.current.querySelectorAll<HTMLElement>('[data-key-idx]');
    const modelNodes = containerRef.current.querySelectorAll<HTMLElement>('[data-model-idx]');

    if (!gatewayEl) return;
    const gwRect = gatewayEl.getBoundingClientRect();
    const gwX = gwRect.left + gwRect.width / 2 - container.left;
    const gwY = gwRect.top + gwRect.height / 2 - container.top;

    const newPaths: PathDef[] = [];

    models.forEach((_, mi) => {
      const modelEl = modelNodes[mi];
      if (!modelEl) return;
      const modelRect = modelEl.getBoundingClientRect();
      const modelX = modelRect.left - container.left;
      const modelY = modelRect.top + modelRect.height / 2 - container.top;

      if (keys.length > 0) {
        keys.forEach((_, ki) => {
          const keyEl = keyNodes[ki];
          if (!keyEl) return;
          const keyRect = keyEl.getBoundingClientRect();
          const keyX = keyRect.right - container.left;
          const keyY = keyRect.top + keyRect.height / 2 - container.top;

          // Key -> Gateway
          const dx1 = Math.abs(gwX - keyX);
          const cp1 = dx1 * 0.4;
          newPaths.push({
            d: `M ${keyX} ${keyY} C ${keyX + cp1} ${keyY}, ${gwX - cp1} ${gwY}, ${gwX} ${gwY}`,
            keyIdx: ki,
            modelIdx: mi,
          });

          // Gateway -> Model
          const dx2 = Math.abs(modelX - gwX);
          const cp2 = dx2 * 0.4;
          newPaths.push({
            d: `M ${gwX} ${gwY} C ${gwX + cp2} ${gwY}, ${modelX - cp2} ${modelY}, ${modelX} ${modelY}`,
            keyIdx: ki,
            modelIdx: mi,
          });
        });
      } else {
        // Passthrough: Gateway -> Model only
        const dx = Math.abs(modelX - gwX);
        const cp = dx * 0.4;
        newPaths.push({
          d: `M ${gwX} ${gwY} C ${gwX + cp} ${gwY}, ${modelX - cp} ${modelY}, ${modelX} ${modelY}`,
          keyIdx: null,
          modelIdx: mi,
        });
      }
    });

    setPaths(newPaths);
  }, [keys, models]);

  useEffect(() => {
    const timer = setTimeout(calculatePaths, 80);
    window.addEventListener('resize', calculatePaths);
    return () => {
      clearTimeout(timer);
      window.removeEventListener('resize', calculatePaths);
    };
  }, [calculatePaths]);

  return (
    <div ref={containerRef} className="absolute inset-0 pointer-events-none">
      <svg className="w-full h-full overflow-visible">
        <defs>
          <filter id="arl-glow" x="-20%" y="-20%" width="140%" height="140%" filterUnits="userSpaceOnUse">
            <feGaussianBlur stdDeviation="2" result="blur" />
            <feComposite in="SourceGraphic" in2="blur" operator="over" />
          </filter>
        </defs>
        {paths.map((p, i) => {
          const isKeyHovered = hoveredKey !== null && p.keyIdx === hoveredKey;
          const isModelHovered = hoveredModel === p.modelIdx;
          const isHighlighted = isKeyHovered || isModelHovered;
          const isDimmed = (hoveredKey !== null || hoveredModel !== null) && !isHighlighted;
          const color = p.keyIdx !== null
            ? PATH_COLORS[p.keyIdx % PATH_COLORS.length]
            : '#6b7280';

          return (
            <path
              key={i}
              d={p.d}
              fill="none"
              stroke={color}
              strokeWidth={isHighlighted ? 2.5 : 1}
              strokeOpacity={isHighlighted ? 0.7 : isDimmed ? 0.08 : 0.25}
              strokeLinecap="round"
              filter={isHighlighted ? 'url(#arl-glow)' : undefined}
              className="transition-all duration-300"
            />
          );
        })}
      </svg>
    </div>
  );
}
