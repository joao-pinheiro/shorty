import { Link } from 'react-router-dom';

export function NotFound() {
  return (
    <div className="text-center py-20">
      <h2 className="text-2xl font-bold text-gray-900 mb-2">404</h2>
      <p className="text-gray-600 mb-4">Page not found.</p>
      <Link to="/" className="text-blue-600 hover:text-blue-800">
        Go to Dashboard
      </Link>
    </div>
  );
}
