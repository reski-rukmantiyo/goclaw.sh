import { describe, it, expect } from "vitest";
import { isValidWhatsAppPhoneJid } from "./contact-jid";

describe("isValidWhatsAppPhoneJid", () => {
  it("accepts international phone JIDs", () => {
    expect(isValidWhatsAppPhoneJid("628123456789@s.whatsapp.net")).toBe(true);
  });

  it("trims surrounding whitespace before validating", () => {
    expect(isValidWhatsAppPhoneJid("  628123456789@s.whatsapp.net ")).toBe(true);
  });

  it("rejects local-format numbers without the JID suffix", () => {
    expect(isValidWhatsAppPhoneJid("081581484242")).toBe(false);
  });

  it("rejects LID-form JIDs", () => {
    expect(isValidWhatsAppPhoneJid("98765432109876@lid")).toBe(false);
  });

  it("rejects group JIDs", () => {
    expect(isValidWhatsAppPhoneJid("120363409245069998@g.us")).toBe(false);
  });

  it("rejects non-digit or empty numbers", () => {
    expect(isValidWhatsAppPhoneJid("+628123456789@s.whatsapp.net")).toBe(false);
    expect(isValidWhatsAppPhoneJid("@s.whatsapp.net")).toBe(false);
    expect(isValidWhatsAppPhoneJid("")).toBe(false);
  });
});
