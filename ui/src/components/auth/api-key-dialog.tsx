import { useState } from 'react';
import {
	Dialog,
	DialogContent,
	DialogHeader,
	DialogTitle,
	DialogDescription,
} from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Key, Loader2, Eye, EyeOff } from 'lucide-react';

interface ApiKeyDialogProps {
	open: boolean;
	onClose: () => void;
	provider: string;
	providerName: string;
	onSubmit: (key: string) => Promise<void>;
}

export function ApiKeyDialog({ open, onClose, providerName, onSubmit }: ApiKeyDialogProps) {
	const [key, setKey] = useState('');
	const [show, setShow] = useState(false);
	const [loading, setLoading] = useState(false);
	const [error, setError] = useState<string | null>(null);

	const handleSubmit = async () => {
		if (!key.trim()) return;
		setLoading(true);
		setError(null);
		try {
			await onSubmit(key.trim());
			setKey('');
			onClose();
		} catch (e) {
			setError(e instanceof Error ? e.message : 'Failed to register key');
		} finally {
			setLoading(false);
		}
	};

	return (
		<Dialog open={open} onOpenChange={(v) => !v && onClose()}>
			<DialogContent className="sm:max-w-md">
				<DialogHeader>
					<DialogTitle className="flex items-center gap-2">
						<Key className="h-4 w-4" />
						Add {providerName} API Key
					</DialogTitle>
					<DialogDescription>
						The API key will be stored securely and used for request routing.
					</DialogDescription>
				</DialogHeader>

				<div className="space-y-4 mt-2">
					<div className="relative">
						<Input
							type={show ? 'text' : 'password'}
							value={key}
							onChange={(e) => setKey(e.target.value)}
							placeholder="Enter API key..."
							className="font-mono pr-10"
							onKeyDown={(e) => e.key === 'Enter' && handleSubmit()}
							autoFocus
						/>
						<button
							type="button"
							onClick={() => setShow(!show)}
							className="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
						>
							{show ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
						</button>
					</div>

					{error && (
						<p className="text-sm text-red-500">{error}</p>
					)}

					<div className="flex gap-2">
						<Button variant="outline" onClick={onClose} disabled={loading} className="flex-1">
							Cancel
						</Button>
						<Button onClick={handleSubmit} disabled={!key.trim() || loading} className="flex-1">
							{loading ? <Loader2 className="h-4 w-4 animate-spin mr-1" /> : <Key className="h-4 w-4 mr-1" />}
							Register
						</Button>
					</div>
				</div>
			</DialogContent>
		</Dialog>
	);
}
