export type SortDirection = 'asc' | 'desc';

export interface SortState {
  column: string;
  direction: SortDirection;
}

export function parseSortParams(
  searchParams: URLSearchParams,
  defaultColumn: string,
  defaultDirection: SortDirection = 'desc',
): SortState {
  const column = searchParams.get('sort') || defaultColumn;
  const dir = searchParams.get('dir');
  const direction: SortDirection = dir === 'asc' || dir === 'desc' ? dir : defaultDirection;
  return { column, direction };
}

export function applySortParams(
  searchParams: URLSearchParams,
  setSearchParams: (params: URLSearchParams) => void,
  column: string,
  current: SortState,
  resetPage?: boolean,
): void {
  const next = new URLSearchParams(searchParams);
  if (current.column === column) {
    next.set('dir', current.direction === 'asc' ? 'desc' : 'asc');
  } else {
    next.set('sort', column);
    next.set('dir', 'desc');
  }
  if (resetPage) {
    next.set('page', '1');
  }
  setSearchParams(next);
}

export function sortRows<T>(
  rows: T[],
  sortState: SortState,
  accessors: Record<string, (row: T) => string | number>,
): T[] {
  const accessor = accessors[sortState.column];
  if (!accessor) return rows;
  const sorted = [...rows].sort((a, b) => {
    const aVal = accessor(a);
    const bVal = accessor(b);
    if (typeof aVal === 'number' && typeof bVal === 'number') {
      return aVal - bVal;
    }
    return String(aVal).localeCompare(String(bVal));
  });
  return sortState.direction === 'desc' ? sorted.reverse() : sorted;
}
