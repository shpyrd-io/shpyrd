import { defineConfig, configDefaults } from "vitest/config";
import { editorialMdx } from "@shpyrd/shared/editorial/vite";
export default defineConfig({ plugins: [editorialMdx()], test: { exclude: [...configDefaults.exclude, ".next/**", "out/**"] } });
