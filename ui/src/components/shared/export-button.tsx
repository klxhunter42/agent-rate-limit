import { Download } from 'lucide-react';
import { Button } from '@/components/ui/button';

interface ExportButtonProps {
  data: Record<string, unknown>[];
  filename: string;
  format?: 'csv' | 'json';
  label?: string;
}

function toCSV(data: Record<string, unknown>[]): string {
  if (data.length === 0) return '';
  const headers = Object.keys(data[0] ?? {});
  const rows = data.map((row) =>
    headers.map((h) => JSON.stringify(row[h] ?? '')).join(','),
  );
  return [headers.join(','), ...rows].join('\n');
}

function download(content: string, filename: string, mime: string) {
  const blob = new Blob([content], { type: mime });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
}

export function ExportButton({ data, filename, format = 'csv', label = 'Export' }: ExportButtonProps) {
  const handleExport = () => {
    if (data.length === 0) return;

    if (format === 'json') {
      download(JSON.stringify(data, null, 2), `${filename}.json`, 'application/json');
    } else {
      download(toCSV(data), `${filename}.csv`, 'text/csv');
    }
  };

  return (
    <Button variant="outline" size="sm" onClick={handleExport} disabled={data.length === 0}>
      <Download className="h-4 w-4 mr-1" />
      {label}
    </Button>
  );
}
