import { Card, CardContent } from '@/components/ui/card';
import { cn } from '@/lib/utils';
import type { LucideIcon } from 'lucide-react';

const variantStyles: Record<string, { iconBg: string; iconColor: string; hoverBorder: string }> = {
  default: { iconBg: 'bg-muted', iconColor: 'text-muted-foreground', hoverBorder: 'hover:border-border' },
  success: { iconBg: 'bg-emerald-500/10', iconColor: 'text-emerald-500', hoverBorder: 'hover:border-emerald-500/30' },
  warning: { iconBg: 'bg-amber-500/10', iconColor: 'text-amber-500', hoverBorder: 'hover:border-amber-500/30' },
  error: { iconBg: 'bg-red-500/10', iconColor: 'text-red-500', hoverBorder: 'hover:border-red-500/30' },
  accent: { iconBg: 'bg-blue-500/10', iconColor: 'text-blue-500', hoverBorder: 'hover:border-blue-500/30' },
};

interface StatCardProps {
  title: string;
  value: string;
  subtitle?: string;
  icon: LucideIcon;
  variant?: 'default' | 'success' | 'warning' | 'error' | 'accent';
  className?: string;
}

export function StatCard({ title, value, subtitle, icon: Icon, variant = 'default', className }: StatCardProps) {
  const style = variantStyles[variant] ?? variantStyles.default;

  return (
    <Card
      className={cn(
        'transition-all duration-200 hover:shadow-md hover:-translate-y-0.5 active:scale-[0.98] border-transparent',
        style?.hoverBorder,
        className,
      )}
    >
      <CardContent className="p-4">
        <div className="flex items-center gap-3">
          <div className={cn('flex items-center justify-center h-10 w-10 rounded-full shrink-0', style?.iconBg)}>
            <Icon className={cn('h-5 w-5', style?.iconColor)} />
          </div>
          <div className="min-w-0 flex-1">
            <p className="text-xs font-medium text-muted-foreground truncate">{title}</p>
            <p className="text-xl font-bold font-mono truncate">{value}</p>
            {subtitle && <p className="text-xs text-muted-foreground truncate">{subtitle}</p>}
          </div>
        </div>
      </CardContent>
    </Card>
  );
}
