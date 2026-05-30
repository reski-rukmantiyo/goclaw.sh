import { z } from "zod";

export const loginSchema = z.object({
  email: z.string().email(),
  password: z.string().min(8),
});

export type LoginForm = z.infer<typeof loginSchema>;

export const passwordChangeSchema = z
  .object({
    current_password: z.string().min(1),
    new_password: z.string().min(12).regex(/[A-Z]/, "Must contain uppercase").regex(/[0-9]/, "Must contain number"),
    confirm_password: z.string().min(12),
  })
  .refine((d) => d.new_password === d.confirm_password, {
    message: "Passwords do not match",
    path: ["confirm_password"],
  });

export type PasswordChangeForm = z.infer<typeof passwordChangeSchema>;

export const groupCreateSchema = z.object({
  name: z.string().min(1).max(255),
  description: z.string().max(1000).optional(),
  parent_group_id: z.string().uuid().optional().nullable(),
  visibility: z.enum(["open", "closed"]),
  max_members: z.number().int().min(0).default(0),
});

export type GroupCreateForm = z.infer<typeof groupCreateSchema>;

export const groupUpdateSchema = z.object({
  name: z.string().min(1).max(255).optional(),
  description: z.string().max(1000).optional().nullable(),
  visibility: z.enum(["open", "closed"]).optional(),
  max_members: z.number().int().min(0).optional(),
});

export type GroupUpdateForm = z.infer<typeof groupUpdateSchema>;
