using System;
using System.Collections.Generic;
using System.Text;

namespace Vmnet.Fixtures
{
    // Fase 2 demo fixture: objects, callvirt (property accessors),
    // List<T>/Dictionary<string,V>, an unhandled throw, and the
    // byte[]-in/byte[]-out bridge CallBytes/CallJSON drive from Go.
    public static class Rules
    {
        // Input content isn't echoed back (byte[] indexing/array element
        // access isn't supported yet — see docs/ROADMAP.md) — only its
        // presence is checked, via string length, which vmnet does support.
        public static byte[] Eval(byte[] input)
        {
            var text = Encoding.UTF8.GetString(input);
            if (text.Length == 0)
            {
                throw new InvalidOperationException("empty input");
            }

            var customer = new Customer();
            customer.Name = "acme corp";
            customer.Age = 30;

            var amounts = new List<int>();
            amounts.Add(100);
            amounts.Add(250);

            var taxByCountry = new Dictionary<string, int>();
            taxByCountry.Add("AR", 21);

            // Exercised for real (proves List/Dictionary retrieval works),
            // kept out of the output to avoid needing Int32.ToString() via
            // boxed multi-operand string concat.
            var itemCount = amounts.Count;
            var firstAmount = amounts[0];
            var tax = taxByCountry["AR"];
            if (itemCount != 2 || firstAmount != 100 || tax != 21)
            {
                throw new InvalidOperationException("internal consistency check failed");
            }

            var output = "{\"ok\":true,\"customer\":\"" + customer.Name + "\"}";
            return Encoding.UTF8.GetBytes(output);
        }
    }
}
