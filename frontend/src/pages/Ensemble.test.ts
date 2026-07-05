import { describe, expect, it } from 'bun:test';

describe('Ensemble header navigation', () => {
  it('includes a desktop link to the admin panel', async () => {
    const source = await Bun.file(new URL('./Ensemble.tsx', import.meta.url)).text();

    expect(source).toContain('href="/admin"');
    expect(source).toContain('Admin Panel');
  });
});
