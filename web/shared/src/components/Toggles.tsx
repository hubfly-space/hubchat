import * as RadixCheckbox from "@radix-ui/react-checkbox";
import * as RadixRadio from "@radix-ui/react-radio-group";
import * as RadixSwitch from "@radix-ui/react-switch";
import { Check, Minus } from "lucide-react";
import { forwardRef, useId, type ReactNode } from "react";
import { cn } from "../lib/cn";

/* -------------------------------------------------------------------------- */
/*  Checkbox                                                                   */
/* -------------------------------------------------------------------------- */

export type CheckboxProps = Omit<
  React.ComponentPropsWithoutRef<typeof RadixCheckbox.Root>,
  "children"
> & {
  label?: ReactNode;
  description?: ReactNode;
};

export const Checkbox = forwardRef<
  React.ElementRef<typeof RadixCheckbox.Root>,
  CheckboxProps
>(function Checkbox({ label, description, className, id, ...props }, ref) {
  const generatedId = useId();
  const controlId = id ?? generatedId;

  const box = (
    <RadixCheckbox.Root
      ref={ref}
      id={controlId}
      className={cn(
        "peer grid size-4 shrink-0 place-items-center rounded-xs border border-line-strong bg-inset",
        "transition-[background-color,border-color] duration-fast ease-out",
        "hover:border-line-loud",
        "data-[state=checked]:border-accent data-[state=checked]:bg-accent data-[state=checked]:text-accent-fg",
        "data-[state=indeterminate]:border-accent data-[state=indeterminate]:bg-accent data-[state=indeterminate]:text-accent-fg",
        "disabled:cursor-not-allowed disabled:border-line disabled:bg-fill disabled:opacity-60",
        !label && className,
      )}
      {...props}
    >
      <RadixCheckbox.Indicator className="grid place-items-center">
        {props.checked === "indeterminate" ? (
          <Minus aria-hidden="true" className="size-3" strokeWidth={3} />
        ) : (
          <Check aria-hidden="true" className="size-3" strokeWidth={3} />
        )}
      </RadixCheckbox.Indicator>
    </RadixCheckbox.Root>
  );

  if (!label) return box;

  return (
    <div className={cn("flex items-start gap-2.5", className)}>
      <span className="mt-px">{box}</span>
      <span className="min-w-0">
        <label
          htmlFor={controlId}
          className="cursor-pointer select-none text-sm text-fg peer-disabled:cursor-not-allowed peer-disabled:text-fg-disabled"
        >
          {label}
        </label>
        {description && (
          <p className="mt-0.5 text-xs leading-snug text-fg-muted">{description}</p>
        )}
      </span>
    </div>
  );
});

/* -------------------------------------------------------------------------- */
/*  Radio                                                                      */
/* -------------------------------------------------------------------------- */

export type RadioOption<T extends string = string> = {
  value: T;
  label: ReactNode;
  description?: ReactNode;
  disabled?: boolean;
};

export type RadioGroupProps<T extends string = string> = {
  value?: T;
  defaultValue?: T;
  onValueChange?: (value: T) => void;
  options: RadioOption<T>[];
  name?: string;
  disabled?: boolean;
  /** "cards" gives each option a selectable surface — for high-stakes choices. */
  variant?: "list" | "cards";
  orientation?: "vertical" | "horizontal";
  className?: string;
  "aria-label"?: string;
};

export function RadioGroup<T extends string = string>({
  value,
  defaultValue,
  onValueChange,
  options,
  name,
  disabled,
  variant = "list",
  orientation = "vertical",
  className,
  ...aria
}: RadioGroupProps<T>) {
  return (
    <RadixRadio.Root
      value={value}
      defaultValue={defaultValue}
      onValueChange={onValueChange as (v: string) => void}
      name={name}
      disabled={disabled}
      aria-label={aria["aria-label"]}
      className={cn(
        orientation === "vertical" ? "flex flex-col gap-2" : "flex flex-wrap items-start gap-4",
        variant === "cards" && orientation === "vertical" && "gap-2",
        className,
      )}
    >
      {options.map((option) => (
        <RadioItem key={option.value} option={option} variant={variant} />
      ))}
    </RadixRadio.Root>
  );
}

function RadioItem({
  option,
  variant,
}: {
  option: RadioOption<string>;
  variant: "list" | "cards";
}) {
  const id = useId();

  const control = (
    <RadixRadio.Item
      id={id}
      value={option.value}
      disabled={option.disabled}
      className={cn(
        "grid size-4 shrink-0 place-items-center rounded-full border border-line-strong bg-inset",
        "transition-[border-color,background-color] duration-fast ease-out",
        "hover:border-line-loud",
        "data-[state=checked]:border-accent data-[state=checked]:bg-accent",
        "disabled:cursor-not-allowed disabled:opacity-60",
      )}
    >
      <RadixRadio.Indicator className="size-1.5 rounded-full bg-accent-fg" />
    </RadixRadio.Item>
  );

  if (variant === "cards") {
    return (
      <label
        htmlFor={id}
        className={cn(
          "group flex cursor-pointer items-start gap-2.5 rounded-md border border-line bg-surface p-3",
          "transition-[border-color,background-color] duration-fast ease-out",
          "hover:border-line-strong hover:bg-surface-hover",
          "has-[[data-state=checked]]:border-accent-border has-[[data-state=checked]]:bg-accent-subtle",
          option.disabled && "cursor-not-allowed opacity-60",
        )}
      >
        <span className="mt-0.5">{control}</span>
        <span className="min-w-0">
          <span className="block text-sm font-medium text-fg">{option.label}</span>
          {option.description && (
            <span className="mt-0.5 block text-xs leading-snug text-fg-muted">
              {option.description}
            </span>
          )}
        </span>
      </label>
    );
  }

  return (
    <div className="flex items-start gap-2.5">
      <span className="mt-px">{control}</span>
      <span className="min-w-0">
        <label htmlFor={id} className="cursor-pointer select-none text-sm text-fg">
          {option.label}
        </label>
        {option.description && (
          <p className="mt-0.5 text-xs leading-snug text-fg-muted">{option.description}</p>
        )}
      </span>
    </div>
  );
}

/* -------------------------------------------------------------------------- */
/*  Switch                                                                     */
/* -------------------------------------------------------------------------- */

export type SwitchProps = React.ComponentPropsWithoutRef<typeof RadixSwitch.Root> & {
  label?: ReactNode;
  description?: ReactNode;
  size?: "sm" | "md";
};

/**
 * A switch commits immediately; a checkbox waits for a Save. Choosing between
 * them is a semantic decision, not a stylistic one — settings that persist on
 * toggle use Switch, form fields that submit later use Checkbox.
 */
export const Switch = forwardRef<React.ElementRef<typeof RadixSwitch.Root>, SwitchProps>(
  function Switch({ label, description, size = "md", className, id, ...props }, ref) {
    const generatedId = useId();
    const controlId = id ?? generatedId;

    const track = (
      <RadixSwitch.Root
        ref={ref}
        id={controlId}
        className={cn(
          "group relative shrink-0 rounded-full border border-line-strong bg-fill",
          "transition-colors duration-base ease-out",
          "data-[state=checked]:border-accent data-[state=checked]:bg-accent",
          "disabled:cursor-not-allowed disabled:opacity-50",
          size === "md" ? "h-[18px] w-8" : "h-4 w-7",
          !label && className,
        )}
        {...props}
      >
        <RadixSwitch.Thumb
          className={cn(
            "block rounded-full bg-fg shadow-1",
            "transition-transform duration-base ease-spring",
            "group-data-[state=checked]:bg-accent-fg",
            size === "md"
              ? "size-3.5 translate-x-0.5 group-data-[state=checked]:translate-x-[15px]"
              : "size-3 translate-x-0.5 group-data-[state=checked]:translate-x-[13px]",
          )}
        />
      </RadixSwitch.Root>
    );

    if (!label) return track;

    return (
      <div className={cn("flex items-start justify-between gap-4", className)}>
        <span className="min-w-0">
          <label htmlFor={controlId} className="cursor-pointer select-none text-sm text-fg">
            {label}
          </label>
          {description && (
            <p className="mt-0.5 text-xs leading-snug text-fg-muted">{description}</p>
          )}
        </span>
        <span className="mt-0.5">{track}</span>
      </div>
    );
  },
);
