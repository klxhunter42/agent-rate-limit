import { Eye, EyeOff } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { usePrivacy } from '@/contexts/privacy-context';
import { cn } from '@/lib/utils';

export function PrivacyToggle() {
  const { privacyMode, togglePrivacyMode } = usePrivacy();

  return (
    <Button variant="ghost" size="icon" onClick={togglePrivacyMode} className="h-8 w-8">
      {privacyMode ? (
        <EyeOff className={cn('h-4 w-4', privacyMode && 'text-amber-500')} />
      ) : (
        <Eye className="h-4 w-4" />
      )}
    </Button>
  );
}
