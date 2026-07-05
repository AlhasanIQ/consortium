import { ArrowDown, ArrowUp, ArrowUpDown } from 'lucide-react';
import { TableHead } from '@/components/ui/table';
import type { SortState } from '@/lib/useSort';
import { cn } from '@/lib/utils';

interface SortableTableHeadProps {
  column: string;
  label: string;
  sort: SortState;
  onSort: (column: string) => void;
  className?: string;
}

export function SortableTableHead({ column, label, sort, onSort, className }: SortableTableHeadProps) {
  const isActive = sort.column === column;

  return (
    <TableHead
      className={cn('cursor-pointer select-none hover:text-foreground/80', className)}
      onClick={() => onSort(column)}
    >
      <span className="inline-flex items-center gap-1">
        {label}
        {isActive ? (
          sort.direction === 'asc' ? (
            <ArrowUp className="h-3.5 w-3.5" />
          ) : (
            <ArrowDown className="h-3.5 w-3.5" />
          )
        ) : (
          <ArrowUpDown className="h-3.5 w-3.5 opacity-30" />
        )}
      </span>
    </TableHead>
  );
}
