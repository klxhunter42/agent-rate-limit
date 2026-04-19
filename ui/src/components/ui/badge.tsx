import { cn } from '@/lib/utils';

function Badge({ className, variant = 'default', children }: {
  className?: string;
  variant?: 'default' | 'secondary' | 'outline' | 'destructive';
  children: React.ReactNode;
}) {
  const variants: Record<string, string> = {
    default: 'bg-primary text-primary-foreground',
    secondary: 'bg-secondary text-secondary-foreground',
    outline: 'border border-input text-foreground',
    destructive: 'bg-destructive text-destructive-foreground',
  };

  return (
    <span
      className={cn(
        'inline-flex items-center rounded-md px-2 py-0.5 text-xs font-medium',
        variants[variant],
        className
      )}
    >
      {children}
    </span>
  );
}

export { Badge };
