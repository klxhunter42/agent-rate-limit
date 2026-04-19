import { cn } from '@/lib/utils';

function Skeleton({ className, ...props }: React.ComponentProps<'div'>) {
  return (
    <div
      data-slot="skeleton"
      className={cn(
        'relative overflow-hidden bg-muted/50 before:absolute before:inset-0 before:-translate-x-full before:bg-gradient-to-r before:from-transparent before:via-muted/30 before:to-transparent before:animate-[shimmer_2s_infinite_ease-out] rounded-md',
        className
      )}
      {...props}
    />
  );
}

export { Skeleton };
