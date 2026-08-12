import { Sun, Moon } from "lucide-react";
import { useTheme } from "@/context/theme";
import { Button } from "@/components/ui/button";

export default function ThemeToggle() {
  const { theme, toggleTheme } = useTheme();
  return (
    <Button
      variant="ghost"
      size="icon"
      onClick={toggleTheme}
      aria-label={theme === "dark" ? "Cambiar a tema claro" : "Cambiar a tema oscuro"}
      className="bg-gradient-to-b from-sky-400 to-blue-600 text-white shadow-[0_4px_14px_rgba(59,130,246,0.35)] hover:from-sky-300 hover:to-blue-500 hover:text-white"
    >
      {theme === "dark" ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
    </Button>
  );
}
