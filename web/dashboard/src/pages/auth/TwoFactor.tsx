import { Button, Field, Input } from "@hubchat/shared";
import { ShieldCheck } from "lucide-react";
import { useRef, useState } from "react";
import { Link } from "react-router-dom";

const LENGTH = 6;

export default function TwoFactor() {
  const [digits, setDigits] = useState<string[]>(Array(LENGTH).fill(""));
  const [useRecovery, setUseRecovery] = useState(false);
  const inputs = useRef<(HTMLInputElement | null)[]>([]);

  const setDigit = (index: number, value: string) => {
    const cleaned = value.replace(/\D/g, "");
    if (!cleaned) return;

    setDigits((current) => {
      const next = [...current];
      // Paste of a full code fills every box rather than only the focused one.
      cleaned.split("").forEach((char, offset) => {
        if (index + offset < LENGTH) next[index + offset] = char;
      });
      return next;
    });

    const target = Math.min(index + cleaned.length, LENGTH - 1);
    inputs.current[target]?.focus();
  };

  return (
    <>
      <div className="mb-4 grid size-11 place-items-center rounded-xl border border-line bg-surface">
        <ShieldCheck aria-hidden="true" className="size-5 text-accent-text" />
      </div>

      <h1 className="text-xl font-semibold tracking-tight text-fg">Two-factor authentication</h1>
      <p className="mt-2 text-sm text-fg-muted">
        {useRecovery
          ? "Enter one of the recovery codes you saved when enabling two-factor."
          : "Enter the six-digit code from your authenticator app."}
      </p>

      <form className="mt-6 flex flex-col gap-4">
        {useRecovery ? (
          <Field label="Recovery code" htmlFor="recovery">
            <Input id="recovery" mono inputSize="lg" placeholder="xxxx-xxxx-xxxx" autoFocus />
          </Field>
        ) : (
          <div>
            <fieldset>
              <legend className="sr-only">Six-digit verification code</legend>
              <div className="flex gap-2">
                {digits.map((digit, index) => (
                  <input
                    key={index}
                    ref={(element) => {
                      inputs.current[index] = element;
                    }}
                    value={digit}
                    onChange={(event) => setDigit(index, event.target.value)}
                    onKeyDown={(event) => {
                      if (event.key === "Backspace" && !digits[index]) {
                        inputs.current[index - 1]?.focus();
                      }
                    }}
                    inputMode="numeric"
                    autoComplete={index === 0 ? "one-time-code" : "off"}
                    maxLength={LENGTH}
                    aria-label={`Digit ${index + 1}`}
                    autoFocus={index === 0}
                    className="h-12 w-full rounded-md border border-line bg-inset text-center font-mono text-lg text-fg outline-none transition-colors focus:border-accent focus:bg-surface focus:shadow-[0_0_0_3px_var(--hc-accent-subtle)]"
                  />
                ))}
              </div>
            </fieldset>
          </div>
        )}

        <Button type="submit" variant="primary" size="lg" fullWidth>
          Verify
        </Button>
      </form>

      <div className="mt-6 flex flex-col gap-2 text-center text-xs">
        <button
          type="button"
          onClick={() => setUseRecovery((current) => !current)}
          className="text-accent-text hover:underline"
        >
          {useRecovery ? "Use your authenticator app instead" : "Use a recovery code instead"}
        </button>
        <Link to="/login" className="text-fg-muted hover:text-fg">
          Back to sign in
        </Link>
      </div>
    </>
  );
}
