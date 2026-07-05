import {
  BarChart3,
  ExternalLink,
  FlaskConical,
  KeyRound,
  LayoutDashboard,
  ListChecks,
  Network,
  RefreshCcw,
  SlidersHorizontal,
} from 'lucide-react';
import { NavLink, Outlet } from 'react-router-dom';
import { cn } from '@/lib/utils';

const links = [
  { to: '/admin', label: 'Overview', icon: LayoutDashboard, end: true },
  { to: '/admin/api', label: 'API', icon: KeyRound },
  { to: '/admin/jobs', label: 'Jobs', icon: ListChecks },
  { to: '/admin/workflows', label: 'Workflows', icon: Network },
  { to: '/admin/benchmarks', label: 'Benchmarks', icon: BarChart3 },
  { to: '/admin/optimize', label: 'Optimize', icon: SlidersHorizontal },
  { to: '/admin/benchloop', label: 'Benchloop', icon: RefreshCcw },
  { to: '/admin/testing', label: 'Testing', icon: FlaskConical },
];

export default function AdminLayout() {
  return (
    <div className="admin-theme text-foreground flex h-full w-full overflow-hidden bg-[radial-gradient(1200px_600px_at_100%_-10%,rgba(14,165,233,0.08),transparent),radial-gradient(900px_600px_at_-10%_120%,rgba(20,184,166,0.08),transparent)]">
      {/* Sidebar */}
      <aside className="bg-sidebar text-sidebar-foreground border-sidebar-border hidden w-64 shrink-0 flex-col border-r md:flex">
        <div className="p-5 pb-0">
          <div className="mb-6">
            <p className="text-sidebar-foreground/60 text-[10px] font-semibold uppercase tracking-[0.25em]">
              Consortium
            </p>
            <h1 className="text-xl font-bold tracking-tight">Admin</h1>
          </div>
          <nav className="space-y-0.5">
            {links.map((link) => {
              const Icon = link.icon;
              return (
                <NavLink
                  key={link.to}
                  end={link.end}
                  to={link.to}
                  className={({ isActive }) =>
                    cn(
                      'flex items-center gap-2.5 rounded-lg px-3 py-2 text-sm font-medium transition-all duration-150',
                      isActive
                        ? 'bg-sidebar-accent text-sidebar-accent-foreground ring-sidebar-ring/35 shadow-sm ring-1 ring-inset'
                        : 'text-sidebar-foreground/70 hover:bg-sidebar-accent/70 hover:text-sidebar-foreground',
                    )
                  }
                >
                  <Icon className="h-4 w-4 shrink-0" />
                  {link.label}
                </NavLink>
              );
            })}
          </nav>
        </div>

        {/* Sidebar footer */}
        <div className="border-sidebar-border/70 mt-auto border-t p-4">
          <a
            href="/ensemble"
            className="text-sidebar-foreground/65 hover:bg-sidebar-accent/60 hover:text-sidebar-foreground flex items-center gap-2 rounded-lg px-3 py-2 text-xs font-medium transition-colors"
          >
            <ExternalLink className="h-3.5 w-3.5" />
            Open Ensemble
          </a>
        </div>
      </aside>

      {/* Main content */}
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="bg-background/85 border-border/80 border-b px-4 py-3 backdrop-blur-sm md:px-8">
          <div className="flex items-center justify-between">
            <div>
              <h2 className="text-muted-foreground text-sm font-semibold uppercase tracking-[0.18em]">
                Operations Console
              </h2>
            </div>
            <a
              className="bg-card text-card-foreground border-border hover:bg-accent hover:text-accent-foreground hidden items-center gap-1.5 rounded-full border px-4 py-1.5 text-sm font-medium shadow-sm transition sm:inline-flex"
              href="/ensemble"
            >
              Open Ensemble
              <ExternalLink className="text-muted-foreground h-3 w-3" />
            </a>
          </div>
          {/* Mobile nav */}
          <nav className="mt-2.5 flex gap-1.5 overflow-x-auto md:hidden">
            {links.map((link) => (
              <NavLink
                key={`mobile-${link.to}`}
                end={link.end}
                to={link.to}
                className={({ isActive }) =>
                  cn(
                    'shrink-0 rounded-full border px-3 py-1.5 text-xs font-semibold uppercase tracking-wide transition-colors',
                    isActive
                      ? 'border-primary bg-primary text-primary-foreground'
                      : 'bg-card text-muted-foreground border-border hover:bg-accent hover:text-accent-foreground',
                  )
                }
              >
                {link.label}
              </NavLink>
            ))}
          </nav>
        </header>
        <main className="min-h-0 flex-1 overflow-y-auto overflow-x-auto p-4 md:p-8">
          <div className="mx-auto w-full max-w-[1500px] animate-[fadeIn_0.2s_ease-out]">
            <Outlet />
          </div>
        </main>
      </div>
    </div>
  );
}
