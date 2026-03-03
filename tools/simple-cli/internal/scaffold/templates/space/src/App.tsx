export default function App() {
  const style = { fontFamily: 'system-ui, sans-serif', padding: '2rem' }

  return (
    <div style={style}>
      <h1>{'{{.DisplayName}}'}</h1>
      <p>Your Simple Space is ready. Start building!</p>
    </div>
  )
}
