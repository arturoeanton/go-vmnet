using FluentValidation;

namespace VmnetFvDemo
{
    public class Customer
    {
        public string Name { get; set; }
        public int Age { get; set; }
    }

    // RuleFor(c => c.Name) compiles to a real System.Linq.Expressions
    // tree (Expression<Func<Customer, string>>) that FluentValidation
    // itself both WALKS (to find the property name for error messages)
    // and genuinely COMPILES AND INVOKES (to read the actual value being
    // validated on every object it validates) — not just inspected the
    // way DocumentFormat.OpenXml's own ConfigureMetadata uses an
    // expression tree elsewhere in this project.
    //
    // GreaterThanOrEqualTo(18) (Fase 3.68) exercises a genuinely
    // different, harder path than NotEmpty: it dispatches through
    // AbstractComparisonValidator<T,TProperty>, a generic base class with
    // TWO same-named, same-arity IsValid overrides down its own
    // hierarchy (itself and GreaterThanOrEqualValidator<T,TProperty>)
    // that only differ by full signature — real .NET tells them apart by
    // vtable slot, which vmnet's own by-name ancestor walk previously
    // conflated (see docs/en/ROADMAP.md, Fase 3.68).
    public class CustomerValidator : AbstractValidator<Customer>
    {
        public CustomerValidator()
        {
            RuleFor(c => c.Name).NotEmpty().WithMessage("Name is required");
            RuleFor(c => c.Age).GreaterThanOrEqualTo(18);
        }
    }

    public static class Program
    {
        public static string Validate(string name)
        {
            var validator = new CustomerValidator();
            var result = validator.Validate(new Customer { Name = name, Age = 30 });
            if (result.IsValid)
            {
                return "valid";
            }
            return "invalid: " + result.Errors[0].ErrorMessage;
        }

        public static string ValidateAge(int age)
        {
            var validator = new CustomerValidator();
            var result = validator.Validate(new Customer { Name = "Ada", Age = age });
            if (result.IsValid)
            {
                return "valid";
            }
            return "invalid: " + result.Errors[0].ErrorMessage;
        }
    }
}
