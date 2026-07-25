import { Button } from './components/Button';
import { formatDate } from '@myorg/core';

export function App() {
  const d = formatDate();
  return <Button label={d} />;
}
