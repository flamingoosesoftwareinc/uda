import { formatDate } from '@myorg/core/utils';

export function App() {
  const d = formatDate();
  return <div>{d}</div>;
}
