export function benchmarkItemDetailPath(runID: string, itemID: string): string {
  return `/admin/benchmarks/${runID}/items/${encodeURIComponent(itemID)}`;
}
