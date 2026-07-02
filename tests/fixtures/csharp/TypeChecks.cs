namespace Vmnet.Fixtures
{
    // Fase 3.8 golden fixture: real class/interface hierarchy behind
    // isinst/castclass (`is`/`as`/explicit cast).
    public interface IShape
    {
        int Area();
    }

    public class Animal
    {
        public string Name;
    }

    public class Dog : Animal, IShape
    {
        public int Area()
        {
            return 42;
        }
    }

    public class Cat : Animal
    {
    }

    public static class TypeChecks
    {
        private static Animal MakeAnimal(bool dog, string name)
        {
            Animal a = dog ? (Animal)new Dog() : new Cat();
            a.Name = name;
            return a;
        }

        public static bool IsDog(bool dog)
        {
            Animal a = MakeAnimal(dog, "x");
            return a is Dog;
        }

        public static bool ImplementsIShape(bool dog)
        {
            Animal a = MakeAnimal(dog, "x");
            return a is IShape;
        }

        public static string CastToDogName(bool dog)
        {
            Animal a = MakeAnimal(dog, "Rex");
            var d = (Dog)a;
            return d.Name;
        }

        public static bool AsDogSucceeds(bool dog)
        {
            Animal a = MakeAnimal(dog, "x");
            var d = a as Dog;
            return d != null;
        }

        public static bool ArgNullIsArgException()
        {
            object ex = new System.ArgumentNullException("param");
            return ex is System.ArgumentException;
        }

        public static bool ArgNullIsInvalidOp()
        {
            object ex = new System.ArgumentNullException("param");
            return ex is System.InvalidOperationException;
        }
    }
}
