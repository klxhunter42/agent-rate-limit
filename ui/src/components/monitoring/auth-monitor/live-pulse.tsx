export function LivePulse() {
  return (
    <div className="relative flex items-center justify-center w-5 h-5">
      <div className="absolute w-4 h-4 rounded-full animate-ping opacity-20 bg-emerald-500" />
      <div className="absolute w-3 h-3 rounded-full animate-pulse opacity-40 bg-emerald-500" />
      <div className="relative w-2 h-2 rounded-full z-10 bg-emerald-500" />
    </div>
  );
}
