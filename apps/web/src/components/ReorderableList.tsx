import { useEffect, useMemo, useRef, useState, type CSSProperties, type ReactNode } from 'react';
import { DndContext, PointerSensor, closestCenter, useSensor, useSensors, type DragEndEvent } from '@dnd-kit/core';
import { SortableContext, arrayMove, useSortable, verticalListSortingStrategy } from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';
import { useT } from '../app/i18n';
import { Icon } from './Icon';

export interface RowHandle {
  grip: ReactNode;
  setNodeRef: (el: HTMLElement | null) => void;
  style: CSSProperties;
  dragging: boolean;
  /** Move to top / bottom for long lists (R-ORG-6). */
  moveToTop?: () => void;
  moveToBottom?: () => void;
}

type MoveFn<T> = (item: T, afterId: string | null, beforeId: string | null) => Promise<boolean>;

/**
 * An ordered list with manual ordering (R-ORG-6). Dragging shows a preview; the
 * dropped order is only a preview until the server accepts it, and the last
 * accepted order comes back if it rejects the move (§7.6, R-MUT-2). Keyboard: focus
 * a handle and use ↑ ↓. Long lists render incrementally.
 */
export function ReorderableList<T>({
  items,
  getId,
  getName,
  renderRow,
  onMove,
  enabled,
  label,
  className = 'song-list',
  pageSize = 150,
}: {
  items: T[];
  getId: (t: T) => string;
  getName: (t: T) => string;
  renderRow: (item: T, handle: RowHandle) => ReactNode;
  onMove: MoveFn<T>;
  enabled: boolean;
  label: string;
  className?: string;
  pageSize?: number;
}) {
  const [preview, setPreview] = useState<string[] | null>(null);
  const [limit, setLimit] = useState(pageSize);
  const sentinel = useRef<HTMLLIElement>(null);
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 4 } }));

  // The accepted order replaces any preview as soon as it changes.
  const accepted = useMemo(() => items.map(getId), [items, getId]);
  useEffect(() => setPreview(null), [accepted]);

  const byId = useMemo(() => new Map(items.map((i) => [getId(i), i])), [items, getId]);
  const orderIds = preview ?? accepted;
  const ordered = orderIds.map((id) => byId.get(id)).filter((x): x is T => !!x);
  const visible = ordered.slice(0, limit);

  useEffect(() => {
    const el = sentinel.current;
    if (!el || ordered.length <= limit) return;
    const io = new IntersectionObserver((entries) => {
      if (entries.some((e) => e.isIntersecting)) setLimit((l) => l + pageSize);
    });
    io.observe(el);
    return () => io.disconnect();
  }, [ordered.length, limit, pageSize]);

  const move = async (fromIdx: number, toIdx: number) => {
    if (fromIdx === toIdx || toIdx < 0 || toIdx >= orderIds.length) return;
    const next = arrayMove(orderIds, fromIdx, toIdx);
    const id = orderIds[fromIdx]!;
    const after = next[toIdx - 1] ?? null;
    const before = next[toIdx + 1] ?? null;
    setPreview(next);
    const ok = await onMove(byId.get(id)!, after, before);
    if (!ok) setPreview(null);
  };

  const onDragEnd = (e: DragEndEvent) => {
    if (!e.over || e.active.id === e.over.id) return;
    void move(orderIds.indexOf(String(e.active.id)), orderIds.indexOf(String(e.over.id)));
  };

  return (
    <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}>
      <SortableContext items={visible.map(getId)} strategy={verticalListSortingStrategy}>
        <ol aria-label={label} className={className}>
          {visible.map((item) => {
            const id = getId(item);
            const idx = orderIds.indexOf(id);
            return (
              <SortableRow
                key={id}
                id={id}
                name={getName(item)}
                enabled={enabled}
                onKeyMove={(delta) => void move(idx, idx + delta)}
                render={(h) =>
                  renderRow(item, {
                    ...h,
                    moveToTop: enabled && idx > 0 ? () => void move(idx, 0) : undefined,
                    moveToBottom: enabled && idx < orderIds.length - 1 ? () => void move(idx, orderIds.length - 1) : undefined,
                  })
                }
              />
            );
          })}
          {ordered.length > limit && <li ref={sentinel} aria-hidden="true" style={{ height: 1 }} />}
        </ol>
      </SortableContext>
    </DndContext>
  );
}

function SortableRow({ id, name, enabled, onKeyMove, render }: { id: string; name: string; enabled: boolean; onKeyMove: (delta: number) => void; render: (h: RowHandle) => ReactNode }) {
  const t = useT();
  const { attributes, listeners, setNodeRef, setActivatorNodeRef, transform, transition, isDragging } = useSortable({ id, disabled: !enabled });
  const style: CSSProperties = { transform: CSS.Transform.toString(transform), transition };
  const grip = enabled ? (
    <button
      ref={setActivatorNodeRef}
      type="button"
      className="icon-btn grip"
      aria-label={t('common.reorder', { name })}
      {...attributes}
      {...listeners}
      onKeyDown={(e) => {
        if (e.key === 'ArrowUp' || e.key === 'ArrowDown') {
          e.preventDefault();
          onKeyMove(e.key === 'ArrowUp' ? -1 : 1);
          const btn = e.currentTarget;
          setTimeout(() => btn.focus(), 0);
        }
      }}
    >
      <Icon name="grip" />
    </button>
  ) : null;
  return <li ref={setNodeRef} style={style}>{render({ grip, setNodeRef: () => undefined, style: {}, dragging: isDragging })}</li>;
}

/** Plain incremental list for read-only orders (artist pages, search results). */
export function IncrementalList<T>({ items, render, label, pageSize = 150, getId }: { items: T[]; render: (t: T) => ReactNode; label: string; pageSize?: number; getId: (t: T) => string }) {
  const [limit, setLimit] = useState(pageSize);
  const sentinel = useRef<HTMLLIElement>(null);
  useEffect(() => {
    const el = sentinel.current;
    if (!el || items.length <= limit) return;
    const io = new IntersectionObserver((entries) => entries.some((e) => e.isIntersecting) && setLimit((l) => l + pageSize));
    io.observe(el);
    return () => io.disconnect();
  }, [items.length, limit, pageSize]);
  return (
    <ol aria-label={label} className="song-list">
      {items.slice(0, limit).map((i) => (
        <li key={getId(i)}>{render(i)}</li>
      ))}
      {items.length > limit && <li ref={sentinel} aria-hidden="true" style={{ height: 1 }} />}
    </ol>
  );
}
