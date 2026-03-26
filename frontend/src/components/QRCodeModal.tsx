import { useState, useEffect } from 'react';
import { api, AuthError } from '../api/client';

interface QRCodeModalProps {
  isOpen: boolean;
  onClose: () => void;
  linkId: number;
  shortUrl: string;
  code: string;
  onAuthError: () => void;
}

export function QRCodeModal({ isOpen, onClose, linkId, shortUrl, code, onAuthError }: QRCodeModalProps) {
  const [qrUrl, setQrUrl] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!isOpen) return;
    setLoading(true);
    setError(null);

    let objectUrl: string | null = null;

    api.getQRCodeBlob(linkId, 256)
      .then(blob => {
        objectUrl = URL.createObjectURL(blob);
        setQrUrl(objectUrl);
      })
      .catch(err => {
        if (err instanceof AuthError) { onAuthError(); return; }
        setError(err instanceof Error ? err.message : 'Failed to load QR code');
      })
      .finally(() => {
        setLoading(false);
      });

    return () => {
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [isOpen, linkId, onAuthError]);

  if (!isOpen) return null;

  const handleDownload = () => {
    if (!qrUrl) return;
    const a = document.createElement('a');
    a.href = qrUrl;
    a.download = `qr-${code}.png`;
    a.click();
  };

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div className="bg-white rounded-lg shadow-xl p-6 w-full max-w-sm">
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-lg font-semibold">QR Code</h2>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-600">&times;</button>
        </div>

        {loading && <div className="text-center py-8 text-gray-500">Loading...</div>}
        {error && <div className="text-center py-8 text-sm text-red-600">{error}</div>}

        {!loading && !error && qrUrl && (
          <div className="text-center">
            <img
              src={qrUrl}
              alt={`QR code for ${shortUrl}`}
              className="mx-auto border rounded"
              width={256}
              height={256}
            />
            <p className="text-sm text-gray-600 mt-3 font-mono">{shortUrl}</p>
            <button
              onClick={handleDownload}
              className="mt-4 px-4 py-2 text-sm bg-blue-600 text-white rounded hover:bg-blue-700"
            >
              Download PNG
            </button>
          </div>
        )}
      </div>
    </div>
  );
}
