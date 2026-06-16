import { z } from "zod";

export const tokenFormSchema = z.object({
  userId: z.string().min(1, "Required"),
  token: z.string().min(1, "Required"),
});

export type TokenFormData = z.infer<typeof tokenFormSchema>;

export const registerSchema = z.object({
  email: z.string().email("Invalid email"),
  password: z.string().min(8, "Password must be at least 8 characters"),
  displayName: z.string().min(1, "Display name is required"),
});

export type RegisterFormData = z.infer<typeof registerSchema>;
