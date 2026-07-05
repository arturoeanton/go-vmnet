using FluentValidation;

namespace VmnetFvDemo
{
    public class Customer
    {
        public string Name { get; set; }
    }

    // RuleFor(c => c.Name) compiles to a real System.Linq.Expressions
    // tree (Expression<Func<Customer, string>>) that FluentValidation
    // itself both WALKS (to find the property name for error messages)
    // and genuinely COMPILES AND INVOKES (to read the actual value being
    // validated on every object it validates) — not just inspected the
    // way DocumentFormat.OpenXml's own ConfigureMetadata uses an
    // expression tree elsewhere in this project.
    public class CustomerValidator : AbstractValidator<Customer>
    {
        public CustomerValidator()
        {
            RuleFor(c => c.Name).NotEmpty().WithMessage("Name is required");
        }
    }

    public static class Program
    {
        public static string Validate(string name)
        {
            var validator = new CustomerValidator();
            var result = validator.Validate(new Customer { Name = name });
            if (result.IsValid)
            {
                return "valid";
            }
            return "invalid: " + result.Errors[0].ErrorMessage;
        }
    }
}
